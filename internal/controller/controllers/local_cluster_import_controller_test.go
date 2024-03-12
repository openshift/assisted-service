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
	"github.com/openshift/assisted-service/internal/testing"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Reconcile", func() {

	var (
		ctx                context.Context
		cluster            *testing.FakeCluster
		client             clnt.Client
		agentServiceConfig *aiv1beta1.AgentServiceConfig
		managedCluster     *clusterv1.ManagedCluster
		key                clnt.ObjectKey
		request            ctrl.Request
		reconciler         *LocalClusterImportReconciler
	)

	var awaitManagedClusterTriggeredReconcile = func() {
		Eventually(func(g Gomega) {
			_, err := reconciler.Reconcile(ctx, request)
			g.Expect(err).ToNot(HaveOccurred())
		})
	}

	var awaitAgentServiceConfigTriggeredReconcile = func(reason string, status corev1.ConditionStatus, message *string) {
		Eventually(func(g Gomega) {
			_, err := reconciler.Reconcile(ctx, request)
			g.Expect(err).ToNot(HaveOccurred())
			err = client.Get(ctx, key, agentServiceConfig)
			g.Expect(err).ToNot(HaveOccurred())
			condition := conditionsv1.FindStatusCondition(
				agentServiceConfig.Status.Conditions,
				aiv1beta1.ConditionReconcileCompleted,
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

	var triggerAgentServiceConfigReconciliation = func() {
		awaitAgentServiceConfigTriggeredReconcile(aiv1beta1.ReasonReconcileSucceeded, corev1.ConditionTrue, nil)
	}

	var triggerManagedClusterReconciliation = func() {
		awaitManagedClusterTriggeredReconcile()
	}

	var createManagedCluster = func() {
		// Create a ManagedCluster resource
		err := client.Create(ctx, managedCluster)
		Expect(err).ToNot(HaveOccurred())
	}

	var createAgentServiceConfig = func() {
		err := client.Create(ctx, agentServiceConfig)
		Expect(err).ToNot(HaveOccurred())
	}

	var createClusterVersion = func() {
		clusterVersion := &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Status: configv1.ClusterVersionStatus{
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
		err := client.Create(ctx, clusterVersion)
		Expect(err).ToNot(HaveOccurred())
	}

	lb_ext_kubeconfig := []byte(`{"test":"foo_kubeconfig"}`)

	var createApiServerKubeConfigSecret = func() {
		kubeConfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-kubeconfigs",
				Namespace: "openshift-kube-apiserver",
			},
			Data: map[string][]byte{"lb-ext.kubeconfig": lb_ext_kubeconfig},
		}
		err := client.Create(ctx, kubeConfigSecret)
		Expect(err).ToNot(HaveOccurred())
	}

	docker_config_json := []byte(`{"test":"foo"}`)
	docker_config_encoded := make([]byte, base64.StdEncoding.EncodedLen(len(docker_config_json)))
	base64.StdEncoding.Encode(docker_config_encoded, docker_config_json)

	var createMachineApiPullSecret = func() {
		//kubeConfigSecret, err := i.clusterImportOperations.GetSecret("openshift-kube-apiserver", "node-kubeconfigs")
		pullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret",
				Namespace: "openshift-machine-api",
			},
			Data: map[string][]byte{".dockerconfigjson": docker_config_encoded},
		}
		err := client.Create(ctx, pullSecret)
		Expect(err).ToNot(HaveOccurred())
	}

	var createClusterDNS = func() {
		dns := &configv1.DNS{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: configv1.DNSSpec{
				BaseDomain: "app.foobar.bar",
			},
		}
		err := client.Create(ctx, dns)
		Expect(err).ToNot(HaveOccurred())
	}

	var createProxy = func() {
		proxy := &configv1.Proxy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: configv1.ProxySpec{
				HTTPProxy:  "proxy.foo.bar",
				HTTPSProxy: "secure.proxy.foo.bar",
			},
		}
		err := client.Create(ctx, proxy)
		Expect(err).ToNot(HaveOccurred())
	}

	var createNodes = func() {
		for _, name := range []string{"node-1", "node-2", "node-3"} {
			node := corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"node-role.kubernetes.io/control-plane": "true"},
				},
			}
			err := client.Create(ctx, &node)
			Expect(err).ToNot(HaveOccurred())
		}
	}

	var verifyClusterDeploymentPresent = func() {
		clusterDeployment := hivev1.ClusterDeployment{}
		namespacedName := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Get(ctx, namespacedName, &clusterDeployment)
		Expect(err).ToNot(HaveOccurred())
	}

	var verifyAgentClusterInstallPresent = func() {
		agentClusterInstall := hiveext.AgentClusterInstall{}
		namespacedName := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Get(ctx, namespacedName, &agentClusterInstall)
		Expect(err).ToNot(HaveOccurred())
	}

	var verifyClusterDeploymentNotPresent = func() {
		clusterDeployment := hivev1.ClusterDeployment{}
		namespacedName := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Get(ctx, namespacedName, &clusterDeployment)
		Expect(err).To(HaveOccurred())
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	}

	var verifyAgentClusterInstallNotPresent = func() {
		agentClusterInstall := hiveext.AgentClusterInstall{}
		namespacedName := types.NamespacedName{
			Namespace: "local-cluster",
			Name:      "local-cluster",
		}
		err := client.Get(ctx, namespacedName, &agentClusterInstall)
		Expect(err).To(HaveOccurred())
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	}

	BeforeEach(func() {
		// Create a context for the test:
		ctx = context.Background()

		// Create the fake cluster:
		cluster = testing.NewFakeCluster().
			Logger(logger).
			Build()

		// Register types to the cluster scheme
		utilruntime.Must(clusterv1.AddToScheme(cluster.Scheme()))
		utilruntime.Must(configv1.AddToScheme(cluster.Scheme()))
		utilruntime.Must(hivev1.AddToScheme(cluster.Scheme()))
		utilruntime.Must(hiveext.AddToScheme(cluster.Scheme()))

		// Create the client:
		client = cluster.Client()

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
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				FileSystemStorage: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				ImageStorage: &corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
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

		// Create the reconciler:
		reconciler = &LocalClusterImportReconciler{
			Log:              logrusLogger,
			client:           client,
			LocalClusterName: "local-cluster",
		}
	})

	AfterEach(func() {
		cluster.Stop()
	})

	It("should create cluster import CRs when ManagedCluster and AgentServiceConfig are present", func() {
		createManagedCluster()
		createAgentServiceConfig()
		createNodes()
		createProxy()
		createClusterDNS()
		createMachineApiPullSecret()
		createApiServerKubeConfigSecret()
		createClusterVersion()
		triggerAgentServiceConfigReconciliation()
		verifyClusterDeploymentPresent()
		verifyAgentClusterInstallPresent()
	})

	It("cluster import CR's should not be present after reconcile if AgentServiceConfig is not present", func() {
		createNodes()
		createProxy()
		createClusterDNS()
		createMachineApiPullSecret()
		createApiServerKubeConfigSecret()
		createClusterVersion()
		createManagedCluster()
		triggerManagedClusterReconciliation()
		verifyClusterDeploymentNotPresent()
		verifyAgentClusterInstallNotPresent()
	})

	It("cluster import CR's should not be present after reconcile if ManagedCluster is not present", func() {
		createNodes()
		createProxy()
		createClusterDNS()
		createMachineApiPullSecret()
		createApiServerKubeConfigSecret()
		createClusterVersion()
		createAgentServiceConfig()
		triggerAgentServiceConfigReconciliation()
		verifyClusterDeploymentNotPresent()
		verifyAgentClusterInstallNotPresent()
	})
})
