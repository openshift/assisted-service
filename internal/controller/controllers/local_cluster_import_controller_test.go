package controllers

import (
	"context"
	"encoding/base64"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newLocalClusterImportTestReconciler(scheme *runtime.Scheme, initObjs ...client.Object) *LocalClusterImportReconciler {

	rtoList := []runtime.Object{}
	for i := range initObjs {
		rtoList = append(rtoList, initObjs[i])
	}

	c := fakeclient.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(initObjs...).
		WithRuntimeObjects(rtoList...).Build()
	return &LocalClusterImportReconciler{
		log:                    logrus.New(),
		client:                 c,
		localClusterName:       "local-cluster",
		agentServiceConfigName: "agent",
	}
}

var _ = Describe("Reconcile", func() {

	var (
		ctx                context.Context
		client             clnt.Client
		agentServiceConfig *aiv1beta1.AgentServiceConfig
		managedCluster     *clusterv1.ManagedCluster
		key                clnt.ObjectKey
		request            ctrl.Request
		reconciler         *LocalClusterImportReconciler
	)

	var awaitReconcile = func(conditionType conditionsv1.ConditionType, reason string, status corev1.ConditionStatus, message *string) {
		Eventually(func(g Gomega) {
			_, err := reconciler.Reconcile(ctx, request)
			g.Expect(err).ToNot(HaveOccurred())
			err = client.Get(ctx, key, agentServiceConfig)
			g.Expect(err).ToNot(HaveOccurred())
			condition := conditionsv1.FindStatusCondition(
				agentServiceConfig.Status.Conditions,
				conditionType,
			)
			g.Expect(condition).ToNot(BeNil())
			g.Expect(condition.Status).To(Equal(status))
			g.Expect(condition.Reason).To(Equal(reason))
			if message != nil {
				g.Expect(condition.Message).To(ContainSubstring(
					*message,
				))
			}
		}).Should(Succeed())
	}

	BeforeEach(func() {
		// Create a context for the test:
		ctx = context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(clusterv1.AddToScheme(scheme))
		utilruntime.Must(configv1.AddToScheme(scheme))
		utilruntime.Must(hivev1.AddToScheme(scheme))
		utilruntime.Must(hiveext.AddToScheme(scheme))
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(aiv1beta1.AddToScheme(scheme))

		//Create objects to be reconciled / watched.
		agentServiceConfig = &aiv1beta1.AgentServiceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "agent",
			},
			Spec: aiv1beta1.AgentServiceConfigSpec{
				DatabaseStorage: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				FileSystemStorage: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				ImageStorage: &corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
			},
		}
		key = clnt.ObjectKeyFromObject(agentServiceConfig)
		managedCluster = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "local-cluster",
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient:     true,
				LeaseDurationSeconds: 60,
			},
		}
		request = ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: agentServiceConfig.Name,
			},
		}

		node1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-1",
				Labels: map[string]string{"node-role.kubernetes.io/control-plane": "true"},
			},
		}
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-2",
				Labels: map[string]string{"node-role.kubernetes.io/control-plane": "true"},
			},
		}
		node3 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-3",
				Labels: map[string]string{"node-role.kubernetes.io/control-plane": "true"},
			},
		}

		proxy := &configv1.Proxy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: configv1.ProxySpec{
				HTTPProxy:  "proxy.foo.bar",
				HTTPSProxy: "secure.proxy.foo.bar",
			},
		}

		dns := &configv1.DNS{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: configv1.DNSSpec{
				BaseDomain: "app.foobar.bar",
			},
		}

		kubeConfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-kubeconfigs",
				Namespace: "openshift-kube-apiserver",
			},
			Data: map[string][]byte{"lb-ext.kubeconfig": []byte(`{"test":"foo_kubeconfig"}`)},
		}
		docker_config_json := []byte(`{"test":"foo"}`)
		docker_config_encoded := make([]byte, base64.StdEncoding.EncodedLen(len(docker_config_json)))
		base64.StdEncoding.Encode(docker_config_encoded, docker_config_json)
		machineApiPullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret",
				Namespace: "openshift-config",
			},
			Data: map[string][]byte{".dockerconfigjson": docker_config_encoded},
		}

		clusterVersion := &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Status: configv1.ClusterVersionStatus{
				Desired: configv1.Release{
					Image:   "registry.ci.openshift.org/ocp/release@sha256:3512c62a8c5bb232b018153e7d5bfac9bbc047f83e5ea645a41c33144df6c735",
					Version: "4.15.0-0.nightly-2024-04-04-000322",
				},
				History: []configv1.UpdateHistory{
					{
						CompletionTime: &metav1.Time{Time: time.Now()},
						Image:          "registry.ci.openshift.org/ocp/release@sha256:3512c62a8c5bb232b018153e7d5bfac9bbc047f83e5ea645a41c33144df6c735",
						StartedTime:    metav1.Time{Time: time.Now()},
						State:          configv1.CompletedUpdate,
						Verified:       false,
						Version:        "4.15.0-0.nightly-2024-03-12-010512",
					},
				},
			},
		}
		reconciler = newLocalClusterImportTestReconciler(scheme, managedCluster, agentServiceConfig, node1, node2, node3, proxy, dns, kubeConfigSecret, clusterVersion, machineApiPullSecret)
		client = reconciler.client
	})

	It("should create cluster import CRs when ManagedCluster and AgentServiceConfig are present", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedName := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err = client.Get(ctx, namespacedName, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedName, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())
	})

	It("cluster import CR's should not be present after reconcile if AgentServiceConfig is not present", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Delete(ctx, agentServiceConfig)
		Expect(err).ToNot(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, clusterDeployment)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	It("cluster import CR's should not be present after reconcile if ManagedCluster is not present", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Delete(ctx, managedCluster)
		Expect(err).ToNot(HaveOccurred())
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterNotManaged, corev1.ConditionFalse, nil)
		err = client.Get(ctx, namespacedname, clusterDeployment)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	It("should remove cluster import CR's when AgentServiceConfig is removed", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())
		err = client.Delete(ctx, agentServiceConfig)
		Expect(err).ToNot(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, clusterDeployment)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	It("should remove cluster import CR's when ManagedCluster is removed", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())
		err = client.Delete(ctx, managedCluster)
		Expect(err).ToNot(HaveOccurred())
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterNotManaged, corev1.ConditionFalse, nil)
		err = client.Get(ctx, namespacedname, clusterDeployment)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	It("should monitor for proxy change and update AgentClusterInstall correctly", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())

		//Test a change of proxy settings
		proxy := &configv1.Proxy{}
		err = client.Get(ctx, types.NamespacedName{
			Name: "cluster",
		}, proxy)
		Expect(err).ToNot(HaveOccurred())
		proxy.Spec.HTTPProxy = "some.http.uri"
		proxy.Spec.HTTPSProxy = "some.https.uri"
		err = client.Update(ctx, proxy)
		Expect(err).ToNot(HaveOccurred())
		Expect(agentClusterInstall.Spec.Proxy.HTTPProxy).ToNot(Equal("some.http.uri"))
		Expect(agentClusterInstall.Spec.Proxy.HTTPSProxy).ToNot(Equal("some.https.uri"))
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())
		Expect(agentClusterInstall.Spec.Proxy.HTTPProxy).To(Equal("some.http.uri"))
		Expect(agentClusterInstall.Spec.Proxy.HTTPSProxy).To(Equal("some.https.uri"))
	})

	It("should monitor for DNS change and update ClusterDeployment correctly", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())

		//Test a change of DNS settings
		dns := &configv1.DNS{}
		err = client.Get(ctx, types.NamespacedName{
			Name: "cluster",
		}, dns)
		Expect(err).ToNot(HaveOccurred())
		dns.Spec.BaseDomain = "new.base.domain"
		err = client.Update(ctx, dns)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterDeployment.Spec.BaseDomain).ToNot(Equal("new.base.domain"))
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err = client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterDeployment.Spec.BaseDomain).To(Equal("new.base.domain"))
	})

	It("should monitor for Cluster Version change and update ClusterDeployment correctly", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())

		//Test that a change to ClusterVersion changes the cluster image set correctly.
		clusterVersion := &configv1.ClusterVersion{}
		err = client.Get(ctx, types.NamespacedName{
			Name: "version",
		}, clusterVersion)
		Expect(err).ToNot(HaveOccurred())

		clusterVersion.Status.Desired = configv1.Release{
			Image:   "new.clusterversion.image/foo/bar",
			Version: "1.2.3-foobar",
		}
		clusterVersion.Status.History = append([]configv1.UpdateHistory{{
			CompletionTime: &metav1.Time{Time: time.Now()},
			Image:          "new.clusterversion.image/foo/bar",
			StartedTime:    metav1.Time{Time: time.Now()},
			State:          configv1.CompletedUpdate,
			Verified:       false,
			Version:        "1.2.3-foobar",
		}}, clusterVersion.Status.History...)
		err = client.Status().Update(ctx, clusterVersion)

		Expect(err).ToNot(HaveOccurred())
		clusterImageSetName := agentClusterInstall.Spec.ImageSetRef.Name
		clusterImageSet := &hivev1.ClusterImageSet{}
		err = client.Get(ctx, types.NamespacedName{
			Name: clusterImageSetName,
		}, clusterImageSet)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterImageSet.Spec.ReleaseImage).To(Equal("registry.ci.openshift.org/ocp/release@sha256:3512c62a8c5bb232b018153e7d5bfac9bbc047f83e5ea645a41c33144df6c735"))
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err = client.Get(ctx, types.NamespacedName{
			Name: clusterImageSetName,
		}, clusterImageSet)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterImageSet.Spec.ReleaseImage).To(Equal("new.clusterversion.image/foo/bar"))
	})

	It("should monitor for changes to secret pull-secret in namespace openshift-config and update pull-secret for clusterdeployment", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())

		// Verify content of pull secret against local-cluster pull-secret before we change it.
		pullSecret := corev1.Secret{}
		err = client.Get(ctx, types.NamespacedName{
			Name:      "pull-secret",
			Namespace: "openshift-config",
		}, &pullSecret)
		Expect(err).ToNot(HaveOccurred())

		Expect(err).ToNot(HaveOccurred())

		localClusterPullSecret := corev1.Secret{}
		err = client.Get(ctx, types.NamespacedName{
			Name:      "pull-secret",
			Namespace: "local-cluster",
		}, &localClusterPullSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(localClusterPullSecret.Data[".dockerconfigjson"]).To(Equal(pullSecret.Data[".dockerconfigjson"]))

		// Update pull secret
		docker_config_json := []byte(`{"test":"bar"}`)
		docker_config_encoded := make([]byte, base64.StdEncoding.EncodedLen(len(docker_config_json)))
		base64.StdEncoding.Encode(docker_config_encoded, docker_config_json)
		pullSecret.Data = map[string][]byte{".dockerconfigjson": docker_config_encoded}
		err = client.Status().Update(ctx, &pullSecret)
		Expect(err).ToNot(HaveOccurred())
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)

		// Check local cluster pull secret after reconcile.
		err = client.Get(ctx, types.NamespacedName{
			Name:      "pull-secret",
			Namespace: "local-cluster",
		}, &localClusterPullSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(err).ToNot(HaveOccurred())
		Expect(localClusterPullSecret.Data[".dockerconfigjson"]).To(Equal(pullSecret.Data[".dockerconfigjson"]))
	})

	It("should monitor for changes to secret node-kubeconfigs in namespace openshift-kube-apiserver", func() {
		clusterDeployment := &hivev1.ClusterDeployment{}
		agentClusterInstall := &hiveext.AgentClusterInstall{}
		namespacedname := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err := client.Get(ctx, namespacedname, clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
		err = client.Get(ctx, namespacedname, agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())

		// Verify content of pull secret against local-cluster pull-secret before we change it.
		nodeKubeConfigsSecret := corev1.Secret{}
		err = client.Get(ctx, types.NamespacedName{
			Name:      "node-kubeconfigs",
			Namespace: "openshift-kube-apiserver",
		}, &nodeKubeConfigsSecret)
		Expect(err).ToNot(HaveOccurred())
		localClusterAdminKubeConfigSecret := corev1.Secret{}
		err = client.Get(ctx, types.NamespacedName{
			Name:      "local-cluster-admin-kubeconfig",
			Namespace: "local-cluster",
		}, &localClusterAdminKubeConfigSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(localClusterAdminKubeConfigSecret.Data["kubeconfig"]).To(Equal(nodeKubeConfigsSecret.Data["lb-ext.kubeconfig"]))

		// Update node kube config and verify that localClusterAdminKubeConfig is updated.
		nodeKubeConfigsSecret.Data = map[string][]byte{"lb-ext.kubeconfig": []byte(`{"test":"bar_kubeconfig"}`)}
		err = client.Status().Update(ctx, &nodeKubeConfigsSecret)
		Expect(err).ToNot(HaveOccurred())
		awaitReconcile(aiv1beta1.ConditionLocalClusterManaged, aiv1beta1.ReasonLocalClusterManaged, corev1.ConditionTrue, nil)
		err = client.Get(ctx, types.NamespacedName{
			Name:      "local-cluster-admin-kubeconfig",
			Namespace: "local-cluster",
		}, &localClusterAdminKubeConfigSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(localClusterAdminKubeConfigSecret.Data["kubeconfig"]).To(Equal(nodeKubeConfigsSecret.Data["lb-ext.kubeconfig"]))
	})
})
