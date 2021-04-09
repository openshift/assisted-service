package subsystem

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	agentv1 "github.com/openshift/hive/apis/hive/v1/agent"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fakeIgnitionConfigOverride = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	badIgnitionConfigOverride  = `bad ignition config`
)

func deployLocalObjectSecretIfNeeded(ctx context.Context, client k8sclient.Client) *corev1.LocalObjectReference {
	err := client.Get(
		ctx,
		types.NamespacedName{Namespace: Options.Namespace, Name: "pull-secret"},
		&corev1.Secret{},
	)
	if apierrors.IsNotFound(err) {
		deployPullSecretResource(ctx, kubeClient, "pull-secret", pullSecret)
	} else {
		Expect(err).To(BeNil())
	}
	return &corev1.LocalObjectReference{
		Name: "pull-secret",
	}
}

func deployPullSecretResource(ctx context.Context, client k8sclient.Client, name, secret string) {
	data := map[string]string{corev1.DockerConfigJsonKey: secret}
	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
		},
		StringData: data,
		Type:       corev1.SecretTypeDockerConfigJson,
	}
	Expect(client.Create(ctx, s)).To(BeNil())
}

func deployClusterDeploymentCRD(ctx context.Context, client k8sclient.Client, spec *hivev1.ClusterDeploymentSpec) {
	err := client.Create(ctx, &hivev1.ClusterDeployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterDeployment",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		},
		Spec: *spec,
	})
	Expect(err).To(BeNil())
}

func updateClusterDeploymentCRD(ctx context.Context, client k8sclient.Client, clusterDeployment *hivev1.ClusterDeployment) {
	err := client.Update(ctx, clusterDeployment)
	Expect(err).To(BeNil())
}

func deployInstallEnvCRD(ctx context.Context, client k8sclient.Client, name string, spec *v1alpha1.InstallEnvSpec) {
	err := client.Create(ctx, &v1alpha1.InstallEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InstallEnv",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
		},
		Spec: *spec,
	})
	Expect(err).To(BeNil())
}

func getAPIVersion() string {
	return fmt.Sprintf("%s/%s", v1alpha1.GroupVersion.Group, v1alpha1.GroupVersion.Version)
}

func getClusterFromDB(
	ctx context.Context, client k8sclient.Client, db *gorm.DB, key types.NamespacedName, timeout int) *common.Cluster {

	var err error
	cluster := &common.Cluster{}
	start := time.Now()
	for time.Duration(timeout)*time.Second > time.Since(start) {
		cluster, err = common.GetClusterFromDBWhere(db, common.UseEagerLoading, common.SkipDeletedRecords, "kube_key_name = ? and kube_key_namespace = ?", key.Name, key.Namespace)
		if err == nil {
			return cluster
		}
		if !gorm.IsRecordNotFoundError(err) {
			Expect(err).To(BeNil())
		}
		getClusterDeploymentCRD(ctx, client, key)
		time.Sleep(time.Second)
	}
	Expect(err).To(BeNil())
	return cluster
}

func getClusterDeploymentCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *hivev1.ClusterDeployment {
	cluster := &hivev1.ClusterDeployment{}
	err := client.Get(ctx, key, cluster)
	Expect(err).To(BeNil())
	return cluster
}

func getInstallEnvCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1alpha1.InstallEnv {
	installEnv := &v1alpha1.InstallEnv{}
	err := client.Get(ctx, key, installEnv)
	Expect(err).To(BeNil())
	return installEnv
}

func getAgentCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1alpha1.Agent {
	agent := &v1alpha1.Agent{}
	err := client.Get(ctx, key, agent)
	Expect(err).To(BeNil())
	return agent
}

func getSecret(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *corev1.Secret {
	secret := &corev1.Secret{}
	err := client.Get(ctx, key, secret)
	Expect(err).To(BeNil())
	return secret
}

// configureLoclAgentClient reassigns the global agentBMClient variable to a client instance using local token auth
func configureLocalAgentClient(clusterID string) {
	if Options.AuthType != auth.TypeLocal {
		Fail(fmt.Sprintf("Agent client shouldn't be configured for local auth when auth type is %s", Options.AuthType))
	}

	key := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      "assisted-installer-local-auth-key",
	}
	secret := getSecret(context.Background(), kubeClient, key)
	privKeyPEM := secret.Data["ec-private-key.pem"]
	tok, err := gencrypto.LocalJWTForKey(clusterID, string(privKeyPEM))
	Expect(err).To(BeNil())

	agentBMClient = client.New(clientcfg(auth.AgentAuthHeaderWriter(tok)))
}

// FindStatusClusterDeploymentCondition is a port of conditionsv1.FindStatusCondition
func FindStatusClusterDeploymentCondition(conditions []hivev1.ClusterDeploymentCondition,
	conditionType hivev1.ClusterDeploymentConditionType) *hivev1.ClusterDeploymentCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func getDefaultClusterDeploymentSpec(secretRef *corev1.LocalObjectReference) *hivev1.ClusterDeploymentSpec {
	return &hivev1.ClusterDeploymentSpec{
		ClusterName: "test-cluster",
		BaseDomain:  "hive.example.com",
		Provisioning: &hivev1.Provisioning{
			InstallConfigSecretRef: &corev1.LocalObjectReference{Name: "cluster-install-config"},
			ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "openshift-v4.7.0"},
			InstallStrategy: &hivev1.InstallStrategy{
				Agent: &agentv1.InstallStrategy{
					Networking: agentv1.Networking{
						MachineNetwork: []agentv1.MachineNetworkEntry{},
						ClusterNetwork: []agentv1.ClusterNetworkEntry{{
							CIDR:       "10.128.0.0/14",
							HostPrefix: 23,
						}},
						ServiceNetwork: []string{"172.30.0.0/16"},
					},
					SSHPublicKey: sshPublicKey,
					ProvisionRequirements: agentv1.ProvisionRequirements{
						ControlPlaneAgents: 3,
						WorkerAgents:       0,
					},
				},
			},
		},
		Platform: hivev1.Platform{
			AgentBareMetal: &agentv1.BareMetalPlatform{
				APIVIP:     "1.2.3.8",
				IngressVIP: "1.2.3.9",
			},
		},
		PullSecretRef: secretRef,
	}
}

func getDefaultClusterDeploymentSNOSpec(secretRef *corev1.LocalObjectReference) *hivev1.ClusterDeploymentSpec {
	return &hivev1.ClusterDeploymentSpec{
		ClusterName: "test-cluster-sno",
		BaseDomain:  "hive.example.com",
		Provisioning: &hivev1.Provisioning{
			InstallConfigSecretRef: &corev1.LocalObjectReference{Name: "cluster-install-config"},
			ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "openshift-v4.8.0"},
			InstallStrategy: &hivev1.InstallStrategy{
				Agent: &agentv1.InstallStrategy{
					Networking: agentv1.Networking{
						MachineNetwork: []agentv1.MachineNetworkEntry{},
						ClusterNetwork: []agentv1.ClusterNetworkEntry{{
							CIDR:       "10.128.0.0/14",
							HostPrefix: 23,
						}},
						ServiceNetwork: []string{"172.30.0.0/16"},
					},
					SSHPublicKey: sshPublicKey,
					ProvisionRequirements: agentv1.ProvisionRequirements{
						ControlPlaneAgents: 1,
						WorkerAgents:       0,
					},
				},
			},
		},
		Platform: hivev1.Platform{
			AgentBareMetal: &agentv1.BareMetalPlatform{},
		},
		PullSecretRef: secretRef,
	}
}

func getDefaultInstallEnvSpec(secretRef *corev1.LocalObjectReference,
	clusterDeployment *hivev1.ClusterDeploymentSpec) *v1alpha1.InstallEnvSpec {
	return &v1alpha1.InstallEnvSpec{
		ClusterRef: &v1alpha1.ClusterReference{
			Name:      clusterDeployment.ClusterName,
			Namespace: Options.Namespace,
		},
		PullSecretRef:    secretRef,
		SSHAuthorizedKey: sshPublicKey,
	}
}

func cleanUP(ctx context.Context, client k8sclient.Client) {
	Expect(client.DeleteAllOf(ctx, &hivev1.ClusterDeployment{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1alpha1.InstallEnv{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1alpha1.Agent{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
}

func setupNewHost(ctx context.Context, hostname string, clusterID strfmt.UUID) *models.Host {
	host := registerNode(ctx, clusterID, hostname)
	generateHWPostStepReply(ctx, host, validHwInfo, hostname)
	generateFAPostStepReply(ctx, host, validFreeAddresses)
	return host
}

var _ = Describe("[kube-api]cluster installation", func() {
	if !Options.EnableKubeAPI {
		return
	}

	ctx := context.Background()

	waitForReconcileTimeout := 30

	AfterEach(func() {
		cleanUP(ctx, kubeClient)
		clearDB()
	})

	It("deploy clusterDeployment with agents and wait for ready", func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, spec)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
		for _, host := range hosts {
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			Eventually(func() error {
				agent := getAgentCRD(ctx, kubeClient, hostkey)
				agent.Spec.Approved = true
				return kubeClient.Update(ctx, agent)
			}, "30s", "10s").Should(BeNil())
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, key).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(Equal(models.ClusterStatusInstalling))
	})

	It("deploy clusterDeployment with agent and update agent", func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, spec)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Hostname = "newhostname"
			agent.Spec.Approved = true
			agent.Spec.InstallationDiskID = sdb.ID
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() string {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			return h.RequestedHostname
		}, "2m", "10s").Should(Equal("newhostname"))
		Eventually(func() string {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			return h.InstallationDiskID
		}, "2m", "10s").Should(Equal(sdb.ID))
		Eventually(func() bool {
			return conditionsv1.IsStatusConditionTrue(getAgentCRD(ctx, kubeClient, key).Status.Conditions, v1alpha1.AgentSyncedCondition)
		}, "2m", "10s").Should(Equal(true))
		Eventually(func() string {
			return getAgentCRD(ctx, kubeClient, key).Status.Inventory.SystemVendor.Manufacturer
		}, "2m", "10s").Should(Equal(validHwInfo.SystemVendor.Manufacturer))
	})

	It("deploy clusterDeployment and installEnv and verify cluster updates", func() {
		installEnvName := "installenv"
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInsufficient))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.NoProxy).Should(Equal(""))
		Expect(cluster.HTTPProxy).Should(Equal(""))
		Expect(cluster.HTTPSProxy).Should(Equal(""))
		Expect(cluster.AdditionalNtpSource).Should(Equal(""))

		installEnvSpec := getDefaultInstallEnvSpec(secretRef, clusterDeploymentSpec)
		installEnvSpec.Proxy = &v1alpha1.Proxy{
			NoProxy:    "192.168.1.1",
			HTTPProxy:  "http://192.168.1.2",
			HTTPSProxy: "http://192.168.1.3",
		}
		installEnvSpec.AdditionalNTPSources = []string{"192.168.1.4"}
		deployInstallEnvCRD(ctx, kubeClient, installEnvName, installEnvSpec)
		installEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      installEnvName,
		}
		// InstallEnv Reconcile takes longer, since it needs to generate the image.
		Eventually(func() string {
			condition := conditionsv1.FindStatusCondition(getInstallEnvCRD(ctx, kubeClient, installEnvKubeName).Status.Conditions, v1alpha1.ImageCreatedCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(Equal(v1alpha1.ImageStateCreated))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.ImageGenerated).Should(Equal(true))
		By("Validate proxy settings.", func() {
			Expect(cluster.NoProxy).Should(Equal("192.168.1.1"))
			Expect(cluster.HTTPProxy).Should(Equal("http://192.168.1.2"))
			Expect(cluster.HTTPSProxy).Should(Equal("http://192.168.1.3"))
		})
		By("Validate additional NTP settings.")
		Expect(cluster.AdditionalNtpSource).Should(ContainSubstring("192.168.1.4"))
		By("InstallEnv image type defaults to minimal-iso.")
		Expect(cluster.ImageInfo.Type).Should(Equal(models.ImageTypeMinimalIso))
	})

	It("deploy clusterDeployment and installEnv with ignition override", func() {
		installEnvName := "installenv"
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInsufficient))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(""))

		installEnvSpec := getDefaultInstallEnvSpec(secretRef, clusterDeploymentSpec)
		installEnvSpec.IgnitionConfigOverride = fakeIgnitionConfigOverride

		deployInstallEnvCRD(ctx, kubeClient, installEnvName, installEnvSpec)
		installEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      installEnvName,
		}
		// InstallEnv Reconcile takes longer, since it needs to generate the image.
		Eventually(func() string {
			condition := conditionsv1.FindStatusCondition(getInstallEnvCRD(ctx, kubeClient, installEnvKubeName).Status.Conditions, v1alpha1.ImageCreatedCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(Equal(v1alpha1.ImageStateCreated))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(fakeIgnitionConfigOverride))
		Expect(cluster.ImageGenerated).Should(Equal(true))
	})

	It("deploy clusterDeployment and installEnv and with an invalid ignition override", func() {
		installEnvName := "installenv"
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInsufficient))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(""))

		installEnvSpec := getDefaultInstallEnvSpec(secretRef, clusterDeploymentSpec)
		installEnvSpec.IgnitionConfigOverride = badIgnitionConfigOverride

		deployInstallEnvCRD(ctx, kubeClient, installEnvName, installEnvSpec)
		installEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      installEnvName,
		}
		Eventually(func() string {
			condition := conditionsv1.FindStatusCondition(getInstallEnvCRD(ctx, kubeClient, installEnvKubeName).Status.Conditions, v1alpha1.ImageCreatedCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "15s", "2s").Should(Equal(v1alpha1.ImageStateFailedToCreate + ": error parsing ignition: config is not valid"))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.IgnitionConfigOverrides).ShouldNot(Equal(fakeIgnitionConfigOverride))
		Expect(cluster.ImageGenerated).Should(Equal(false))

	})

	It("deploy clusterDeployment with install config override", func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInsufficient))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))

		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName)
		installConfigOverrides := `{"controlPlane": {"hyperthreading": "Enabled"}}`
		clusterDeploymentCRD.SetAnnotations(map[string]string{controllers.InstallConfigOverrides: installConfigOverrides})
		updateClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentCRD)

		Eventually(func() string {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
			if c != nil {
				return c.InstallConfigOverrides
			}
			return ""
		}, "1m", "2s").Should(Equal(installConfigOverrides))
	})

	It("deploy clusterDeployment with malformed install config override", func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInsufficient))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))

		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName)
		installConfigOverrides := `{"controlPlane": "malformed json": "Enabled"}}`
		clusterDeploymentCRD.SetAnnotations(map[string]string{controllers.InstallConfigOverrides: installConfigOverrides})
		updateClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentCRD)

		Eventually(func() string {
			// currently all conditions have the same type, so filter by status = False
			conditions := getClusterDeploymentCRD(ctx, kubeClient, clusterKubeName).Status.Conditions
			for i := range conditions {
				if conditions[i].Status == "False" {
					return conditions[i].Message
				}
			}
			return ""
		}, "30s", "2s").Should(ContainSubstring("failed to update clusterDeployment"))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))
	})

	It("SNO deploy clusterDeployment full install and validate MetaData", func() {
		By("Create cluster")
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterDeploymentSNOSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, spec)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Wait for installing")
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInstalling))

		By("Wait for finalizing")
		updateProgress(*host.ID, *cluster.ID, models.HostStageDone)
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusFinalizing))

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.CompleteInstallation(ctx, &installer.CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verify Cluster Metadata")
		Eventually(func() bool {
			return getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.Installed
		}, "1m", "2s").Should(BeTrue())
		passwordSecretRef := getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.ClusterMetadata.AdminPasswordSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		passwordkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      passwordSecretRef.Name,
		}
		passwordSecret := getSecret(ctx, kubeClient, passwordkey)
		Expect(passwordSecret.Data["password"]).NotTo(BeNil())
		Expect(passwordSecret.Data["username"]).NotTo(BeNil())
		configSecretRef := getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		configkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      configSecretRef.Name,
		}
		configSecret := getSecret(ctx, kubeClient, configkey)
		Expect(configSecret.Data["kubeconfig"]).NotTo(BeNil())
	})

	It("None SNO deploy clusterDeployment full install and validate MetaData", func() {
		By("Create cluster")
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, spec)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
		By("Approve Agents")
		for _, host := range hosts {
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			Eventually(func() error {
				agent := getAgentCRD(ctx, kubeClient, hostkey)
				agent.Spec.Approved = true
				return kubeClient.Update(ctx, agent)
			}, "30s", "10s").Should(BeNil())
		}

		By("Wait for installing")
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInstalling))

		By("Wait for finalizing")
		for _, host := range hosts {
			updateProgress(*host.ID, *cluster.ID, models.HostStageDone)
		}

		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusFinalizing))

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.CompleteInstallation(ctx, &installer.CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verify Day 2 Cluster")
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(Equal(models.ClusterStatusAddingHosts))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))

		By("Verify Cluster Metadata")
		Eventually(func() bool {
			return getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.Installed
		}, "2m", "2s").Should(BeTrue())
		passwordSecretRef := getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.ClusterMetadata.AdminPasswordSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		passwordkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      passwordSecretRef.Name,
		}
		passwordSecret := getSecret(ctx, kubeClient, passwordkey)
		Expect(passwordSecret.Data["password"]).NotTo(BeNil())
		Expect(passwordSecret.Data["username"]).NotTo(BeNil())
		configSecretRef := getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		configkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      configSecretRef.Name,
		}
		configSecret := getSecret(ctx, kubeClient, configkey)
		Expect(configSecret.Data["kubeconfig"]).NotTo(BeNil())
	})

	It("None SNO deploy clusterDeployment full install and Day 2 new host", func() {
		By("Create cluster")
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, spec)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
		By("Approve Agents")
		for _, host := range hosts {
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			Eventually(func() error {
				agent := getAgentCRD(ctx, kubeClient, hostkey)
				agent.Spec.Approved = true
				return kubeClient.Update(ctx, agent)
			}, "30s", "10s").Should(BeNil())
		}

		By("Wait for installing")
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusInstalling))

		By("Wait for finalizing")
		for _, host := range hosts {
			updateProgress(*host.ID, *cluster.ID, models.HostStageDone)
		}

		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "1m", "2s").Should(Equal(models.ClusterStatusFinalizing))

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.CompleteInstallation(ctx, &installer.CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verify Day 2 Cluster")
		Eventually(func() string {
			condition := FindStatusClusterDeploymentCondition(getClusterDeploymentCRD(ctx, kubeClient, clusterKey).Status.Conditions, hivev1.UnreachableCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(Equal(models.ClusterStatusAddingHosts))
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))

		By("Add Day 2 host and approve agent")
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostnameday2", *cluster.ID)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		generateApiVipPostStepReply(ctx, host, true)
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		//TODO check Agent status when implemented By("Wait for Day 2 host to be installing")

	})

})
