package controllers

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("reconcileNodeLabelPropagation", func() {
	var (
		c         client.Client
		ir        *InfraEnvReconciler
		ctx       context.Context
		infraEnv  *aiv1beta1.InfraEnv
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-ns"
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(&aiv1beta1.InfraEnv{}).Build()
		ir = &InfraEnvReconciler{
			Client: c,
			Log:    common.GetTestLog(),
		}
	})

	createInfraEnv := func(labels, annotations map[string]string) *aiv1beta1.InfraEnv {
		ie := &aiv1beta1.InfraEnv{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-infraenv",
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
		}
		Expect(c.Create(ctx, ie)).To(Succeed())
		return ie
	}

	createAgent := func(name string, nodeLabels map[string]string, annotations map[string]string) *aiv1beta1.Agent {
		agent := &aiv1beta1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					aiv1beta1.InfraEnvNameLabel: "test-infraenv",
				},
				Annotations: annotations,
			},
			Spec: aiv1beta1.AgentSpec{
				NodeLabels: nodeLabels,
			},
		}
		Expect(c.Create(ctx, agent)).To(Succeed())
		return agent
	}

	getAgent := func(name string) *aiv1beta1.Agent {
		agent := &aiv1beta1.Agent{}
		Expect(c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, agent)).To(Succeed())
		return agent
	}

	Context("propagation from InfraEnv to existing Agents", func() {
		It("propagates labels to all Agents belonging to InfraEnv", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			createAgent("agent-1", nil, nil)
			createAgent("agent-2", nil, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent1 := getAgent("agent-1")
			Expect(agent1.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent1.Spec.NodeLabels).To(HaveKeyWithValue("zone", "us-east-1a"))

			agent2 := getAgent("agent-2")
			Expect(agent2.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent2.Spec.NodeLabels).To(HaveKeyWithValue("zone", "us-east-1a"))
		})

		It("updates labels when InfraEnv label values change", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-1", nil, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("nyc"))

			// Simulate InfraEnv label change
			infraEnv.Labels["site"] = "chicago"
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("chicago"))
		})

		It("removes inherited labels when propagation annotation is removed", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-1", nil, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))

			// Remove the propagation annotation
			delete(infraEnv.Annotations, PropagateNodeLabelsAnnotation)
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("site"))
			Expect(agent.GetAnnotations()).NotTo(HaveKey(InheritedNodeLabelsAnnotation))
		})

		It("does not modify Agents from different InfraEnvs", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-in-scope", nil, nil)

			otherAgent := &aiv1beta1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-out-of-scope",
					Namespace: namespace,
					Labels: map[string]string{
						aiv1beta1.InfraEnvNameLabel: "different-infraenv",
					},
				},
				Spec: aiv1beta1.AgentSpec{},
			}
			Expect(c.Create(ctx, otherAgent)).To(Succeed())

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			outOfScope := getAgent("agent-out-of-scope")
			Expect(outOfScope.Spec.NodeLabels).To(BeEmpty())

			inScope := getAgent("agent-in-scope")
			Expect(inScope.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
		})

		It("preserves user-set labels on conflict", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-1", map[string]string{"site": "user-custom-site"}, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("user-custom-site"))
		})

		It("is idempotent on repeated reconciliation", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			createAgent("agent-1", nil, nil)

			// First reconciliation
			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "us-east-1a"))
			rvAfterFirst := agent.ResourceVersion

			// Fetch fresh copy for second reconcile (to match what the controller would see)
			freshInfraEnv := &aiv1beta1.InfraEnv{}
			Expect(c.Get(ctx, types.NamespacedName{Name: infraEnv.Name, Namespace: infraEnv.Namespace}, freshInfraEnv)).To(Succeed())

			// Second reconciliation should be a no-op — no patch issued
			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, freshInfraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "us-east-1a"))
			Expect(agent.ResourceVersion).To(Equal(rvAfterFirst), "Agent should not be patched on second reconciliation")
		})

		It("handles adding new keys to propagation list", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a", "region": "us-east"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-1", nil, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("zone"))

			// Expand propagation list
			infraEnv.Annotations[PropagateNodeLabelsAnnotation] = "site,zone,region"
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "us-east-1a"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("region", "us-east"))
		})

		It("handles shrinking the propagation list", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a", "region": "us-east"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone,region"},
			)
			createAgent("agent-1", nil, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveLen(3))

			// Shrink propagation list
			infraEnv.Annotations[PropagateNodeLabelsAnnotation] = "site"
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("zone"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("region"))
		})

		It("handles many agents efficiently", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			for i := 0; i < 10; i++ {
				id := strfmt.UUID(uuid.New().String())
				createAgent(id.String(), nil, nil)
			}

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agents := &aiv1beta1.AgentList{}
			Expect(c.List(ctx, agents, client.InNamespace(namespace),
				client.MatchingLabels{aiv1beta1.InfraEnvNameLabel: "test-infraenv"})).To(Succeed())

			for _, agent := range agents.Items {
				Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "nyc"))
			}
		})

		It("does nothing when no propagation annotation exists", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				nil,
			)
			createAgent("agent-1", map[string]string{"custom": "value"}, nil)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("custom", "value"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("site"))
		})

		It("does not overwrite after user takes ownership of previously inherited label", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			createAgent("agent-1", nil, nil)

			// First reconcile: inherit the label
			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("nyc"))

			// User manually overrides the label on the Agent
			agent.Spec.NodeLabels["site"] = "user-override"
			delete(agent.GetAnnotations(), InheritedNodeLabelsAnnotation)
			Expect(c.Update(ctx, agent)).To(Succeed())

			// Second reconcile: should NOT overwrite user's value
			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("user-override"))
		})

		It("handles stale inherited annotation from a different InfraEnv", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			// Agent has a stale inherited annotation with a key this InfraEnv doesn't propagate
			createAgent("agent-1",
				map[string]string{"region": "eu-west", "site": "old-site"},
				map[string]string{InheritedNodeLabelsAnnotation: "region,site"},
			)

			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			// "region" was inherited from old InfraEnv but not in current propagation list — removed
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("region"))
			// "site" is in current propagation list and was previously inherited — updated
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("nyc"))
		})

		It("handles last-write-wins on sequential reconciliations with different states", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			createAgent("agent-1", nil, nil)

			// First reconcile with original state
			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("nyc"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("us-east-1a"))

			// Simulate a different state: labels changed, propagation list shrunk
			infraEnv.Labels["site"] = "chicago"
			delete(infraEnv.Labels, "zone")
			infraEnv.Annotations[PropagateNodeLabelsAnnotation] = "site"
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			// Second reconcile with new state
			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("chicago"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("zone"))
		})

		It("cleanly removes all inherited labels leaving nodeLabels empty", func() {
			infraEnv = createInfraEnv(
				map[string]string{"site": "nyc", "zone": "us-east-1a", "region": "us-east", "rack": "r42", "building": "dc-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone,region,rack,building"},
			)
			createAgent("agent-1", nil, nil)

			// First reconcile: inherit 5 labels
			err := ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent := getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(HaveLen(5))

			// Remove propagation annotation entirely
			delete(infraEnv.Annotations, PropagateNodeLabelsAnnotation)
			Expect(c.Update(ctx, infraEnv)).To(Succeed())

			// Second reconcile: all inherited labels removed
			err = ir.reconcileNodeLabelPropagation(ctx, ir.Log, infraEnv)
			Expect(err).To(BeNil())

			agent = getAgent("agent-1")
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
			Expect(agent.GetAnnotations()).NotTo(HaveKey(InheritedNodeLabelsAnnotation))
		})
	})
})
