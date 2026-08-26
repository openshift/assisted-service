package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("reconcileNodePoolLabelPropagation", func() {
	var (
		c         client.Client
		ir        *InfraEnvReconciler
		ctx       context.Context
		infraEnv  *aiv1beta1.InfraEnv
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-hcp-ns"
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(&aiv1beta1.InfraEnv{}).Build()
		ir = &InfraEnvReconciler{
			Client: c,
			Log:    common.GetTestLog(),
		}
	})

	createInfraEnvWithClusterRef := func(labels, annotations map[string]string, clusterName string) *aiv1beta1.InfraEnv {
		ie := &aiv1beta1.InfraEnv{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-infraenv",
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: aiv1beta1.InfraEnvSpec{
				ClusterRef: &aiv1beta1.ClusterReference{
					Name:      clusterName,
					Namespace: namespace,
				},
			},
		}
		Expect(c.Create(ctx, ie)).To(Succeed())
		return ie
	}

	createNodePool := func(name, clusterName string, nodeLabels map[string]string, annotations map[string]string) *unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "hypershift.openshift.io",
			Version: "v1beta1",
			Kind:    "NodePool",
		})
		np.SetName(name)
		np.SetNamespace(namespace)
		if annotations != nil {
			np.SetAnnotations(annotations)
		}

		spec := map[string]interface{}{
			"clusterName": clusterName,
		}
		if nodeLabels != nil {
			labelsInterface := make(map[string]interface{}, len(nodeLabels))
			for k, v := range nodeLabels {
				labelsInterface[k] = v
			}
			spec["nodeLabels"] = labelsInterface
		}
		_ = unstructured.SetNestedField(np.Object, spec, "spec")

		Expect(c.Create(ctx, np)).To(Succeed())
		return np
	}

	getNodePool := func(name string) *unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "hypershift.openshift.io",
			Version: "v1beta1",
			Kind:    "NodePool",
		})
		Expect(c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, np)).To(Succeed())
		return np
	}

	Context("no-op cases", func() {
		It("does nothing if InfraEnv has no ClusterRef", func() {
			infraEnv = &aiv1beta1.InfraEnv{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infraenv",
					Namespace: namespace,
					Labels:    map[string]string{"site": "nyc"},
					Annotations: map[string]string{
						propagateNodeLabelsAnnotation: "site",
					},
				},
			}
			Expect(c.Create(ctx, infraEnv)).To(Succeed())

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())
		})

		It("does nothing if no NodePools match the cluster name", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-other", "other-cluster", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-other")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(BeEmpty())
		})

		It("does nothing if no propagation annotation is set", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				nil,
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(BeEmpty())
		})
	})

	Context("basic propagation", func() {
		It("propagates designated labels to matching NodePools", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc", "zone": "us-east-1a", "other": "ignored"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)
			createNodePool("np-2", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np1 := getNodePool("np-1")
			labels1, _, _ := unstructured.NestedStringMap(np1.Object, "spec", "nodeLabels")
			Expect(labels1).To(HaveKeyWithValue("site", "nyc"))
			Expect(labels1).To(HaveKeyWithValue("zone", "us-east-1a"))
			Expect(labels1).NotTo(HaveKey("other"))
			Expect(np1.GetAnnotations()[inheritedNodeLabelsAnnotation]).To(ContainSubstring("site"))
			Expect(np1.GetAnnotations()[inheritedNodeLabelsAnnotation]).To(ContainSubstring("zone"))

			np2 := getNodePool("np-2")
			labels2, _, _ := unstructured.NestedStringMap(np2.Object, "spec", "nodeLabels")
			Expect(labels2).To(HaveKeyWithValue("site", "nyc"))
			Expect(labels2).To(HaveKeyWithValue("zone", "us-east-1a"))
		})

		It("does not propagate a key that is missing from InfraEnv labels", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "nyc"))
			Expect(labels).NotTo(HaveKey("zone"))
		})
	})

	Context("label updates", func() {
		It("updates value when InfraEnv label changes", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "nyc"))

			infraEnv.Labels["site"] = "lon"
			err = ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np = getNodePool("np-1")
			labels, _, _ = unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "lon"))
		})

		It("removes inherited label when key is removed from propagation annotation", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc", "zone": "us-east-1a"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			infraEnv.Annotations[propagateNodeLabelsAnnotation] = "site"
			err = ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "nyc"))
			Expect(labels).NotTo(HaveKey("zone"))
		})

		It("cleans up all inherited labels when propagation annotation is removed from InfraEnv", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			delete(infraEnv.Annotations, propagateNodeLabelsAnnotation)
			err = ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(BeEmpty())
			Expect(np.GetAnnotations()).NotTo(HaveKey(inheritedNodeLabelsAnnotation))
		})
	})

	Context("conflict resolution", func() {
		It("preserves user-set labels that conflict with InfraEnv labels", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", map[string]string{"site": "user-value"}, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "user-value"))
		})

		It("preserves user-set labels alongside inherited labels", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", map[string]string{"custom": "user-label"}, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "nyc"))
			Expect(labels).To(HaveKeyWithValue("custom", "user-label"))
		})

		It("can overwrite inherited labels on subsequent reconciliations", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			infraEnv.Labels["site"] = "lon"
			err = ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			labels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
			Expect(labels).To(HaveKeyWithValue("site", "lon"))
		})
	})

	Context("idempotency", func() {
		It("is idempotent on repeated calls with no changes", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc", "zone": "us-east-1a"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone"},
				"my-hc",
			)
			createNodePool("np-1", "my-hc", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np := getNodePool("np-1")
			rv := np.GetResourceVersion()

			err = ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np = getNodePool("np-1")
			Expect(np.GetResourceVersion()).To(Equal(rv))
		})
	})

	Context("multiple NodePools", func() {
		It("propagates to all matching NodePools but not unrelated ones", func() {
			infraEnv = createInfraEnvWithClusterRef(
				map[string]string{"site": "nyc"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
				"my-hc",
			)
			createNodePool("np-match-1", "my-hc", nil, nil)
			createNodePool("np-match-2", "my-hc", nil, nil)
			createNodePool("np-other", "other-cluster", nil, nil)

			err := ir.reconcileNodePoolLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			np1 := getNodePool("np-match-1")
			labels1, _, _ := unstructured.NestedStringMap(np1.Object, "spec", "nodeLabels")
			Expect(labels1).To(HaveKeyWithValue("site", "nyc"))

			np2 := getNodePool("np-match-2")
			labels2, _, _ := unstructured.NestedStringMap(np2.Object, "spec", "nodeLabels")
			Expect(labels2).To(HaveKeyWithValue("site", "nyc"))

			npOther := getNodePool("np-other")
			labelsOther, _, _ := unstructured.NestedStringMap(npOther.Object, "spec", "nodeLabels")
			Expect(labelsOther).To(BeEmpty())
		})
	})

	Context("graceful degradation", func() {
		It("gracefully handles absence of NodePool CRD (no error)", func() {
			noNPClient := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			irNoNP := &InfraEnvReconciler{
				Client: noNPClient,
				Log:    common.GetTestLog(),
			}

			infraEnv = &aiv1beta1.InfraEnv{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infraenv",
					Namespace: namespace,
					Labels:    map[string]string{"site": "nyc"},
					Annotations: map[string]string{
						propagateNodeLabelsAnnotation: "site",
					},
				},
				Spec: aiv1beta1.InfraEnvSpec{
					ClusterRef: &aiv1beta1.ClusterReference{
						Name:      "my-hc",
						Namespace: namespace,
					},
				},
			}

			err := irNoNP.reconcileNodePoolLabelPropagation(ctx, irNoNP.Log, infraEnv)
			Expect(err).To(BeNil())
		})
	})
})
