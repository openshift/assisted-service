package subsystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	hiveext "github.com/openshift/assisted-service/internal/controller/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	agentv1 "github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fakeIgnitionConfigOverride           = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	badIgnitionConfigOverride            = `bad ignition config`
	clusterDeploymentNamePrefix          = "test-cluster"
	clusterAgentClusterInstallNamePrefix = "test-agent-cluster-install"
)

var (
	imageSetsData = map[string]string{
		"openshift-v4.7.0": "quay.io/openshift-release-dev/ocp-release:4.7.2-x86_64",
		"openshift-v4.8.0": "quay.io/openshift-release-dev/ocp-release:4.8.0-fc.0-x86_64",
	}
)

func deployLocalObjectSecretIfNeeded(ctx context.Context, client k8sclient.Client) *corev1.LocalObjectReference {
	err := client.Get(
		ctx,
		types.NamespacedName{Namespace: Options.Namespace, Name: pullSecretName},
		&corev1.Secret{},
	)
	if apierrors.IsNotFound(err) {
		deployPullSecretResource(ctx, kubeClient, pullSecretName, pullSecret)
	} else {
		Expect(err).To(BeNil())
	}
	return &corev1.LocalObjectReference{
		Name: pullSecretName,
	}
}

func deployOrUpdateConfigMap(ctx context.Context, client k8sclient.Client, name string, data map[string]string) *corev1.ConfigMap {
	c := &corev1.ConfigMap{}
	err := client.Get(
		ctx,
		types.NamespacedName{Namespace: Options.Namespace, Name: name},
		c,
	)
	if apierrors.IsNotFound(err) {
		c = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Options.Namespace,
				Name:      name,
			},
			Data: data,
		}
		Expect(client.Create(ctx, c)).To(BeNil())
	} else {
		c.Data = data
		Expect(client.Update(ctx, c)).To(BeNil())
	}
	return c
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

func updateAgentClusterInstallCRD(ctx context.Context, client k8sclient.Client, installkey types.NamespacedName, spec *hiveext.AgentClusterInstallSpec) {
	Eventually(func() error {
		agent := getAgentClusterInstallCRD(ctx, client, installkey)
		agent.Spec = *spec
		return kubeClient.Update(ctx, agent)
	}, "30s", "10s").Should(BeNil())
}

func deployAgentClusterInstallCRD(ctx context.Context, client k8sclient.Client, spec *hiveext.AgentClusterInstallSpec,
	clusterAgentClusterInstallName string) {
	deployClusterImageSetCRD(ctx, client, spec.ImageSetRef)
	err := client.Create(ctx, &hiveext.AgentClusterInstall{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AgentClusterInstall",
			APIVersion: "hiveextension/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      clusterAgentClusterInstallName,
		},
		Spec: *spec,
	})
	Expect(err).To(BeNil())
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

func deployBMHCRD(ctx context.Context, client k8sclient.Client, name string, spec *bmhv1alpha1.BareMetalHostSpec) {
	err := client.Create(ctx, &bmhv1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BareMetalHost",
			APIVersion: "metal3.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
		},
		Spec: *spec,
	})
	Expect(err).To(BeNil())
}

func addAnnotationToAgentClusterInstall(ctx context.Context, client k8sclient.Client, key types.NamespacedName, annotationKey string, annotationValue string) {
	Eventually(func() error {
		agentClusterInstallCRD := getAgentClusterInstallCRD(ctx, client, key)
		agentClusterInstallCRD.SetAnnotations(map[string]string{annotationKey: annotationValue})
		return kubeClient.Update(ctx, agentClusterInstallCRD)
	}, "30s", "10s").Should(BeNil())
}

func deployClusterImageSetCRD(ctx context.Context, client k8sclient.Client, imageSetRef hivev1.ClusterImageSetReference) {
	err := client.Create(ctx, &hivev1.ClusterImageSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterImageSet",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      imageSetRef.Name,
		},
		Spec: hivev1.ClusterImageSetSpec{
			ReleaseImage: imageSetsData[imageSetRef.Name],
		},
	})
	Expect(err).To(BeNil())
}

func deployInfraEnvCRD(ctx context.Context, client k8sclient.Client, name string, spec *v1beta1.InfraEnvSpec) {
	err := client.Create(ctx, &v1beta1.InfraEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InfraEnv",
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

func deployNMStateConfigCRD(ctx context.Context, client k8sclient.Client, name string, NMStateLabelName string, NMStateLabelValue string, spec *v1beta1.NMStateConfigSpec) {
	err := client.Create(ctx, &v1beta1.NMStateConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NMStateConfig",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
			Labels:    map[string]string{NMStateLabelName: NMStateLabelValue},
		},
		Spec: *spec,
	})
	Expect(err).To(BeNil())
}

func getAPIVersion() string {
	return fmt.Sprintf("%s/%s", v1beta1.GroupVersion.Group, v1beta1.GroupVersion.Version)
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
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			Expect(err).To(BeNil())
		}
		getClusterDeploymentCRD(ctx, client, key)
		time.Sleep(time.Second)
	}
	Expect(err).To(BeNil())
	return cluster
}

func getClusterDeploymentAgents(ctx context.Context, client k8sclient.Client, clusterDeployment types.NamespacedName) *v1beta1.AgentList {
	agents := &v1beta1.AgentList{}
	clusterAgents := &v1beta1.AgentList{}
	err := client.List(ctx, agents)
	Expect(err).To(BeNil())
	clusterAgents.TypeMeta = agents.TypeMeta
	clusterAgents.ListMeta = agents.ListMeta
	for _, agent := range agents.Items {
		if agent.Spec.ClusterDeploymentName.Name == clusterDeployment.Name &&
			agent.Spec.ClusterDeploymentName.Namespace == clusterDeployment.Namespace {
			clusterAgents.Items = append(clusterAgents.Items, agent)
		}
	}
	return clusterAgents
}

func getClusterDeploymentCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *hivev1.ClusterDeployment {
	cluster := &hivev1.ClusterDeployment{}
	err := client.Get(ctx, key, cluster)
	Expect(err).To(BeNil())
	return cluster
}

func getAgentClusterInstallCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *hiveext.AgentClusterInstall {
	cluster := &hiveext.AgentClusterInstall{}
	err := client.Get(ctx, key, cluster)
	Expect(err).To(BeNil())
	return cluster
}

func getInfraEnvCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1beta1.InfraEnv {
	infraEnv := &v1beta1.InfraEnv{}
	err := client.Get(ctx, key, infraEnv)
	Expect(err).To(BeNil())
	return infraEnv
}

func getAgentCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1beta1.Agent {
	agent := &v1beta1.Agent{}
	err := client.Get(ctx, key, agent)
	Expect(err).To(BeNil())
	return agent
}

func getBmhCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *bmhv1alpha1.BareMetalHost {
	bmh := &bmhv1alpha1.BareMetalHost{}
	err := client.Get(ctx, key, bmh)
	Expect(err).To(BeNil())
	return bmh
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

func checkAgentCondition(ctx context.Context, hostId string, conditionType conditionsv1.ConditionType, reason string) {
	hostkey := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      hostId,
	}
	Eventually(func() string {
		return conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, hostkey).Status.Conditions, conditionType).Reason
	}, "30s", "10s").Should(Equal(reason))
}

func checkAgentClusterInstallCondition(ctx context.Context, key types.NamespacedName, conditionType string, reason string) {
	Eventually(func() string {
		condition := controllers.FindStatusCondition(getAgentClusterInstallCRD(ctx, kubeClient, key).Status.Conditions, conditionType)
		if condition != nil {
			return condition.Reason
		}
		return ""
	}, "2m", "2s").Should(Equal(reason))
}

func checkInfraEnvCondition(ctx context.Context, key types.NamespacedName, conditionType conditionsv1.ConditionType, message string) {
	Eventually(func() string {
		condition := conditionsv1.FindStatusCondition(getInfraEnvCRD(ctx, kubeClient, key).Status.Conditions, conditionType)
		if condition != nil {
			return condition.Message
		}
		return ""
	}, "2m", "1s").Should(Equal(message))
}

func getDefaultClusterDeploymentSpec(secretRef *corev1.LocalObjectReference) *hivev1.ClusterDeploymentSpec {
	return &hivev1.ClusterDeploymentSpec{
		ClusterName: clusterDeploymentNamePrefix + randomNameSuffix(),
		BaseDomain:  "hive.example.com",
		Platform: hivev1.Platform{
			AgentBareMetal: &agentv1.BareMetalPlatform{},
		},
		PullSecretRef: secretRef,
		ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
			Group:   hiveext.Group,
			Version: hiveext.Version,
			Kind:    "AgentClusterInstall",
			Name:    clusterAgentClusterInstallNamePrefix + randomNameSuffix(),
		},
	}
}

func getDefaultAgentClusterInstallSpec(clusterDeploymentName string) *hiveext.AgentClusterInstallSpec {
	return &hiveext.AgentClusterInstallSpec{
		Networking: hiveext.Networking{
			MachineNetwork: []hiveext.MachineNetworkEntry{},
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "10.128.0.0/14",
				HostPrefix: 23,
			}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  hivev1.ClusterImageSetReference{Name: "openshift-v4.8.0"},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       0,
		},
		APIVIP:               "1.2.3.8",
		IngressVIP:           "1.2.3.9",
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeploymentName},
	}
}

func getDefaultSNOAgentClusterInstallSpec(clusterDeploymentName string) *hiveext.AgentClusterInstallSpec {
	return &hiveext.AgentClusterInstallSpec{
		Networking: hiveext.Networking{
			MachineNetwork: []hiveext.MachineNetworkEntry{{CIDR: "1.2.3.0/24"}},
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "10.128.0.0/14",
				HostPrefix: 23,
			}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  hivev1.ClusterImageSetReference{Name: "openshift-v4.8.0"},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 1,
			WorkerAgents:       0,
		},
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeploymentName},
	}
}

func getDefaultInfraEnvSpec(secretRef *corev1.LocalObjectReference,
	clusterDeployment *hivev1.ClusterDeploymentSpec) *v1beta1.InfraEnvSpec {
	return &v1beta1.InfraEnvSpec{
		ClusterRef: &v1beta1.ClusterReference{
			Name:      clusterDeployment.ClusterName,
			Namespace: Options.Namespace,
		},
		PullSecretRef:    secretRef,
		SSHAuthorizedKey: sshPublicKey,
	}
}

func getDefaultNMStateConfigSpec(nicPrimary, nicSecondary, macPrimary, macSecondary, networkYaml string) *v1beta1.NMStateConfigSpec {
	return &v1beta1.NMStateConfigSpec{
		Interfaces: []*v1beta1.Interface{
			{MacAddress: macPrimary, Name: nicPrimary},
			{MacAddress: macSecondary, Name: nicSecondary},
		},
		NetConfig: v1beta1.NetConfig{Raw: []byte(networkYaml)},
	}
}

func getAgentMac(ctx context.Context, client k8sclient.Client, key types.NamespacedName) string {
	mac := ""
	Eventually(func() bool {
		agent := getAgentCRD(ctx, client, key)
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" {
				mac = agentInterface.MacAddress
				return true
			}
		}
		return false
	}, "60s", "10s").Should(BeTrue())

	return mac
}

func randomNameSuffix() string {
	return fmt.Sprintf("-%s", strings.Split(uuid.New().String(), "-")[0])
}

func cleanUP(ctx context.Context, client k8sclient.Client) {
	Expect(client.DeleteAllOf(ctx, &hivev1.ClusterDeployment{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil()) // Should also delete all agents
	Expect(client.DeleteAllOf(ctx, &hivev1.ClusterImageSet{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1beta1.InfraEnv{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1beta1.NMStateConfig{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &bmhv1alpha1.BareMetalHost{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	ps := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      pullSecretName,
		},
		Type: corev1.SecretTypeDockerConfigJson,
	}
	Expect(client.Delete(ctx, ps)).To(BeNil())
}

func verifyCleanUP(ctx context.Context, client k8sclient.Client) {
	By("Verify ClusterDeployment Cleanup")
	Eventually(func() int {
		clusterDeploymentList := &hivev1.ClusterDeploymentList{}
		err := client.List(ctx, clusterDeploymentList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(clusterDeploymentList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify AgentClusterInstall Cleanup")
	Eventually(func() int {
		aciList := &hiveext.AgentClusterInstallList{}
		err := client.List(ctx, aciList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(aciList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify ClusterImageSet Cleanup")
	Eventually(func() int {
		clusterImageSetList := &hivev1.ClusterImageSetList{}
		err := client.List(ctx, clusterImageSetList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(clusterImageSetList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify InfraEnv Cleanup")
	Eventually(func() int {
		infraEnvList := &v1beta1.InfraEnvList{}
		err := client.List(ctx, infraEnvList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(infraEnvList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify NMStateConfig Cleanup")
	Eventually(func() int {
		nmStateConfigList := &v1beta1.NMStateConfigList{}
		err := client.List(ctx, nmStateConfigList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(nmStateConfigList.Items)
	}, "2m", "10s").Should(Equal(0))

	By("Verify Agent Cleanup")
	Eventually(func() int {
		agentList := &v1beta1.AgentList{}
		err := client.List(ctx, agentList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(agentList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify BareMetalHost Cleanup")
	Eventually(func() int {
		bareMetalHostList := &bmhv1alpha1.BareMetalHostList{}
		err := client.List(ctx, bareMetalHostList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(bareMetalHostList.Items)
	}, "2m", "2s").Should(Equal(0))
}

func setupNewHost(ctx context.Context, hostname string, clusterID strfmt.UUID) *models.Host {
	host := registerNode(ctx, clusterID, hostname)
	generateHWPostStepReply(ctx, host, validHwInfo, hostname)
	generateFAPostStepReply(ctx, host, validFreeAddresses)
	generateEssentialPrepareForInstallationSteps(ctx, host)
	return host
}

const waitForReconcileTimeout = 30

var _ = Describe("[kube-api]cluster installation", func() {
	if !Options.EnableKubeAPI {
		return
	}

	ctx := context.Background()

	var (
		clusterDeploymentSpec *hivev1.ClusterDeploymentSpec
		infraEnvSpec          *v1beta1.InfraEnvSpec
		infraNsName           types.NamespacedName
		aciSpec               *hiveext.AgentClusterInstallSpec
		aciSNOSpec            *hiveext.AgentClusterInstallSpec
	)

	BeforeEach(func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec = getDefaultClusterDeploymentSpec(secretRef)
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSNOSpec = getDefaultSNOAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)

		infraNsName = types.NamespacedName{
			Name:      "infraenv",
			Namespace: Options.Namespace,
		}
		infraEnvSpec = getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec)
	})

	AfterEach(func() {
		cleanUP(ctx, kubeClient)
		verifyCleanUP(ctx, kubeClient)
		clearDB()
	})

	It("deploy CD with ACI and agents - wait for ready, delete CD and verify ACI and agents deletion", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), controllers.ValidatedCondition, controllers.ValidationsFailingReason)
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterAlreadyInstallingReason)
		By("Delete ClusterDeployment")
		err := kubeClient.Delete(ctx, getClusterDeploymentCRD(ctx, kubeClient, clusterKey))
		Expect(err).To(BeNil())
		By("Verify AgentClusterInstall was deleted")
		Eventually(func() bool {
			aci := &hiveext.AgentClusterInstall{}
			err := kubeClient.Get(ctx, installkey, aci)
			return apierrors.IsNotFound(err)
		}, "30s", "10s").Should(Equal(true))
		By("Verify ClusterDeployment Agents were deleted")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))
	})

	It("deploy CD with ACI and agents - wait for ready, delete ACI only and verify agents deletion", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), controllers.ValidatedCondition, controllers.ValidationsFailingReason)
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterAlreadyInstallingReason)
		By("Delete AgentClusterInstall")
		err := kubeClient.Delete(ctx, getAgentClusterInstallCRD(ctx, kubeClient, installkey))
		Expect(err).To(BeNil())
		By("Verify ClusterDeployment Agents were deleted")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))
	})

	It("deploy clusterDeployment with agent and update agent", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
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
			return conditionsv1.IsStatusConditionTrue(getAgentCRD(ctx, kubeClient, key).Status.Conditions, controllers.SpecSyncedCondition)
		}, "2m", "10s").Should(Equal(true))
		Eventually(func() string {
			return getAgentCRD(ctx, kubeClient, key).Status.Inventory.SystemVendor.Manufacturer
		}, "2m", "10s").Should(Equal(validHwInfo.SystemVendor.Manufacturer))
	})

	It("deploy clusterDeployment with agent,bmh and ignition config override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := bmhv1alpha1.BareMetalHostSpec{BootMACAddress: getAgentMac(ctx, kubeClient, key)}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_IGNITION_CONFIG_OVERRIDES: fakeIgnitionConfigOverride})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			agent := getAgentCRD(ctx, kubeClient, key)

			if agent.Spec.IgnitionConfigOverrides == "" {
				return false
			}

			if h.IgnitionConfigOverrides == "" {
				return false
			}
			return reflect.DeepEqual(h.IgnitionConfigOverrides, agent.Spec.IgnitionConfigOverrides)
		}, "2m", "10s").Should(Equal(true))

		By("Clean ignition config override ")
		h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.IgnitionConfigOverrides).NotTo(BeEmpty())

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_IGNITION_CONFIG_OVERRIDES: ""})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		By("Verify agent ignition config override were cleaned")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Spec.IgnitionConfigOverrides
		}, "30s", "10s").Should(BeEmpty())

		By("Verify host ignition config override were cleaned")
		Eventually(func() int {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())

			return len(h.IgnitionConfigOverrides)
		}, "2m", "10s").Should(Equal(0))
	})

	It("deploy clusterDeployment with agent and invalid ignition config", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.IgnitionConfigOverrides).To(BeEmpty())

		By("Invalid ignition config - invalid json")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.IgnitionConfigOverrides = badIgnitionConfigOverride
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, key).Status.Conditions, controllers.SpecSyncedCondition)
			if condition != nil {
				return strings.Contains(condition.Message, "error parsing ignition: config is not valid")
			}
			return false
		}, "15s", "2s").Should(Equal(true))
		h, err = common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.IgnitionConfigOverrides).To(BeEmpty())
	})

	It("deploy clusterDeployment with agent and update installer args", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		installerArgs := `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.InstallerArgs = installerArgs
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			agent := getAgentCRD(ctx, kubeClient, key)

			var j, j2 interface{}
			err = json.Unmarshal([]byte(agent.Spec.InstallerArgs), &j)
			Expect(err).To(BeNil())

			if h.InstallerArgs == "" {
				return false
			}

			err = json.Unmarshal([]byte(h.InstallerArgs), &j2)
			Expect(err).To(BeNil())
			return reflect.DeepEqual(j2, j)
		}, "2m", "10s").Should(Equal(true))

		By("Clean installer args")
		h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).NotTo(BeEmpty())

		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.InstallerArgs = ""
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() int {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			var j []string
			err = json.Unmarshal([]byte(h.InstallerArgs), &j)
			Expect(err).To(BeNil())

			return len(j)
		}, "2m", "10s").Should(Equal(0))
	})

	It("deploy clusterDeployment with agent and invalid installer args", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).To(BeEmpty())

		By("Invalid installer args - invalid json")
		installerArgs := `"--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.InstallerArgs = installerArgs
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, key).Status.Conditions, controllers.SpecSyncedCondition)
			if condition != nil {
				return strings.Contains(condition.Message, "Fail to unmarshal installer args")
			}
			return false
		}, "15s", "2s").Should(Equal(true))
		h, err = common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).To(BeEmpty())

		By("Invalid installer args - invalid params")
		installerArgs = `["--wrong-param", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.InstallerArgs = installerArgs
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, key).Status.Conditions, controllers.SpecSyncedCondition)
			if condition != nil {
				return strings.Contains(condition.Message, "found unexpected flag --wrong-param")
			}
			return false
		}, "15s", "2s").Should(Equal(true))
		h, err = common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).To(BeEmpty())
	})

	It("deploy clusterDeployment with agent,bmh and installer args", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := bmhv1alpha1.BareMetalHostSpec{BootMACAddress: getAgentMac(ctx, kubeClient, key)}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)

		installerArgs := `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_INSTALLER_ARGS: installerArgs})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			agent := getAgentCRD(ctx, kubeClient, key)
			if agent.Spec.InstallerArgs == "" {
				return false
			}

			var j, j2 interface{}
			err = json.Unmarshal([]byte(agent.Spec.InstallerArgs), &j)
			Expect(err).To(BeNil())

			if h.InstallerArgs == "" {
				return false
			}

			err = json.Unmarshal([]byte(h.InstallerArgs), &j2)
			Expect(err).To(BeNil())
			return reflect.DeepEqual(j2, j)
		}, "2m", "10s").Should(Equal(true))

		By("Clean installer args")
		h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).NotTo(BeEmpty())

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_INSTALLER_ARGS: ""})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		By("Verify agent installer args were cleaned")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Spec.InstallerArgs
		}, "30s", "10s").Should(BeEmpty())

		By("Verify host installer args were cleaned")
		Eventually(func() int {
			h, err := common.GetHostFromDB(db, cluster.ID.String(), host.ID.String())
			Expect(err).To(BeNil())

			var j []string
			err = json.Unmarshal([]byte(h.InstallerArgs), &j)
			Expect(err).To(BeNil())

			return len(j)
		}, "2m", "10s").Should(Equal(0))
	})

	It("deploy clusterDeployment and infraEnv and verify cluster updates", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.NoProxy).Should(Equal(""))
		Expect(cluster.HTTPProxy).Should(Equal(""))
		Expect(cluster.HTTPSProxy).Should(Equal(""))
		Expect(cluster.AdditionalNtpSource).Should(Equal(""))
		Expect(cluster.Hyperthreading).Should(Equal("all"))

		infraEnvSpec.Proxy = &v1beta1.Proxy{
			NoProxy:    "192.168.1.1",
			HTTPProxy:  "http://192.168.1.2",
			HTTPSProxy: "http://192.168.1.3",
		}
		infraEnvSpec.AdditionalNTPSources = []string{"192.168.1.4"}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.ImageGenerated).Should(Equal(true))
		By("Validate proxy settings.", func() {
			Expect(cluster.NoProxy).Should(Equal("192.168.1.1"))
			Expect(cluster.HTTPProxy).Should(Equal("http://192.168.1.2"))
			Expect(cluster.HTTPSProxy).Should(Equal("http://192.168.1.3"))
		})
		By("Validate additional NTP settings.")
		Expect(cluster.AdditionalNtpSource).Should(ContainSubstring("192.168.1.4"))
		By("InfraEnv image type defaults to minimal-iso.")
		Expect(cluster.ImageInfo.Type).Should(Equal(models.ImageTypeMinimalIso))
	})

	It("deploy clusterDeployment and infraEnv with ignition override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)

		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(""))

		infraEnvSpec.IgnitionConfigOverride = fakeIgnitionConfigOverride

		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(fakeIgnitionConfigOverride))
		Expect(cluster.ImageGenerated).Should(Equal(true))
	})

	It("deploy infraEnv before clusterDeployment", func() {
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())

		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.ImageGenerated).Should(Equal(true))
	})

	It("deploy clusterDeployment and infraEnv and with an invalid ignition override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		Expect(cluster.IgnitionConfigOverrides).Should(Equal(""))

		infraEnvSpec.IgnitionConfigOverride = badIgnitionConfigOverride
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateFailedToCreate+": error parsing ignition: config is not valid")
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.IgnitionConfigOverrides).ShouldNot(Equal(fakeIgnitionConfigOverride))
		Expect(cluster.ImageGenerated).Should(Equal(false))

	})

	It("deploy clusterDeployment with hyperthreading configuration", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		verifyHyperthreadingSetup := func(mode string) {
			Eventually(func() string {
				c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
				return c.Hyperthreading
			}, "1m", "10s").Should(Equal(mode))
		}

		By("new deployment with hyperthreading disabled")
		aciSpec.ControlPlane = &hiveext.AgentMachinePool{
			Hyperthreading: hiveext.HyperthreadingDisabled,
		}
		aciSpec.Compute = []hiveext.AgentMachinePool{
			{Hyperthreading: hiveext.HyperthreadingDisabled},
		}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingNone)

		By("update deployment with hyperthreading enabled on master only")
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.ControlPlane = &hiveext.AgentMachinePool{
			Hyperthreading: hiveext.HyperthreadingEnabled,
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingMasters)

		By("update deployment with hyperthreading enabled on workers only")
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.Compute = []hiveext.AgentMachinePool{
			{Hyperthreading: hiveext.HyperthreadingEnabled},
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingWorkers)
	})

	It("deploy clusterDeployment with install config override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))

		installConfigOverrides := `{"controlPlane": {"hyperthreading": "Enabled"}}`
		addAnnotationToAgentClusterInstall(ctx, kubeClient, installkey, controllers.InstallConfigOverrides, installConfigOverrides)

		Eventually(func() string {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
			if c != nil {
				return c.InstallConfigOverrides
			}
			return ""
		}, "1m", "2s").Should(Equal(installConfigOverrides))
	})

	It("deploy clusterDeployment with malformed install config override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))

		installConfigOverrides := `{"controlPlane": "malformed json": "Enabled"}}`
		addAnnotationToAgentClusterInstall(ctx, kubeClient, installkey, controllers.InstallConfigOverrides, installConfigOverrides)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.InputErrorReason)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))
	})

	It("deploy clusterDeployment and infraEnv and with NMState config", func() {
		var (
			NMStateLabelName  = "someName"
			NMStateLabelValue = "someValue"
			nicPrimary        = "eth0"
			nicSecondary      = "eth1"
			macPrimary        = "09:23:0f:d8:92:AA"
			macSecondary      = "09:23:0f:d8:92:AB"
			ip4Primary        = "192.168.126.30"
			ip4Secondary      = "192.168.140.30"
			dnsGW             = "192.168.126.1"
		)
		hostStaticNetworkConfig := common.FormatStaticConfigHostYAML(
			nicPrimary, nicSecondary, ip4Primary, ip4Secondary, dnsGW,
			models.MacInterfaceMap{
				{MacAddress: macPrimary, LogicalNicName: nicPrimary},
				{MacAddress: macSecondary, LogicalNicName: nicSecondary},
			})
		nmstateConfigSpec := getDefaultNMStateConfigSpec(nicPrimary, nicSecondary, macPrimary, macSecondary, hostStaticNetworkConfig.NetworkYaml)
		deployNMStateConfigCRD(ctx, kubeClient, "nmstate1", NMStateLabelName, NMStateLabelValue, nmstateConfigSpec)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		infraEnvSpec.NMStateConfigLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.ImageInfo.StaticNetworkConfig).Should(ContainSubstring(hostStaticNetworkConfig.NetworkYaml))
		Expect(cluster.ImageGenerated).Should(Equal(true))
	})

	It("deploy clusterDeployment and infraEnv and with an invalid NMState config YAML", func() {
		Skip("MGMT-5324 flaky test")
		var (
			NMStateLabelName  = "someName"
			NMStateLabelValue = "someValue"
			nicPrimary        = "eth0"
			nicSecondary      = "eth1"
			macPrimary        = "09:23:0f:d8:92:AA"
			macSecondary      = "09:23:0f:d8:92:AB"
		)
		nmstateConfigSpec := getDefaultNMStateConfigSpec(nicPrimary, nicSecondary, macPrimary, macSecondary, "foo: bar")
		deployNMStateConfigCRD(ctx, kubeClient, "nmstate2", NMStateLabelName, NMStateLabelValue, nmstateConfigSpec)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		infraEnvSpec.NMStateConfigLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, fmt.Sprintf("%s: internal error", v1beta1.ImageStateFailedToCreate))
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.ImageGenerated).Should(Equal(false))
	})

	It("SNO deploy clusterDeployment full install and validate MetaData", func() {
		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		// Add space suffix to SSHPublicKey to validate proper install
		sshPublicKeySuffixSpace := fmt.Sprintf("%s ", aciSNOSpec.SSHPublicKey)
		aciSNOSpec.SSHPublicKey = sshPublicKeySuffixSpace
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Check ACI Event URL exists")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.EventsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Check Agent Event URL exists")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.EventsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Wait for installing")
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstallationInProgressReason)

		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling, models.HostStatusDisabled}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		updateProgress(*host.ID, *cluster.ID, models.HostStageDone)

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
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstalledReason)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterFailedCondition, controllers.ClusterNotFailedReason)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterStoppedCondition, controllers.ClusterStoppedCompletedReason)

		passwordSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminPasswordSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		passwordkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      passwordSecretRef.Name,
		}
		passwordSecret := getSecret(ctx, kubeClient, passwordkey)
		Expect(passwordSecret.Data["password"]).NotTo(BeNil())
		Expect(passwordSecret.Data["username"]).NotTo(BeNil())
		configSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		configkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      configSecretRef.Name,
		}
		configSecret := getSecret(ctx, kubeClient, configkey)
		Expect(configSecret.Data["kubeconfig"]).NotTo(BeNil())
		// By("Check Event URL does not exist")
		// Eventually(func() string {
		// 	aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
		// 	return aci.Status.DebugInfo.EventsURL
		// }, "1m", "10s").Should(Equal(""))
	})

	It("None SNO deploy clusterDeployment full install and validate MetaData", func() {
		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), controllers.ValidatedCondition, controllers.ValidationsFailingReason)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), controllers.ValidatedCondition, controllers.ValidationsPassingReason)
		}

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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstallationInProgressReason)
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling, models.HostStatusDisabled}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), controllers.InstalledCondition, controllers.InstallationInProgressReason)
		}

		for _, host := range hosts {
			updateProgress(*host.ID, *cluster.ID, models.HostStageDone)
		}

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
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstalledReason)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))

		By("Verify ClusterDeployment Agents were deleted")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))

		By("Verify Cluster Metadata")
		passwordSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminPasswordSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		passwordkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      passwordSecretRef.Name,
		}
		passwordSecret := getSecret(ctx, kubeClient, passwordkey)
		Expect(passwordSecret.Data["password"]).NotTo(BeNil())
		Expect(passwordSecret.Data["username"]).NotTo(BeNil())
		configSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
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
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstallationInProgressReason)
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling, models.HostStatusDisabled}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		for _, host := range hosts {
			updateProgress(*host.ID, *cluster.ID, models.HostStageDone)
		}

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
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterCompletedCondition, controllers.InstalledReason)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))

		By("Verify ClusterDeployment Agents were deleted")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))

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

		checkAgentCondition(ctx, host.ID.String(), controllers.InstalledCondition, controllers.InstallationInProgressReason)
		checkAgentCondition(ctx, host.ID.String(), controllers.ReadyForInstallationCondition, controllers.AgentAlreadyInstallingReason)
		checkAgentCondition(ctx, host.ID.String(), controllers.SpecSyncedCondition, controllers.SyncedOkReason)
		checkAgentCondition(ctx, host.ID.String(), controllers.ConnectedCondition, controllers.AgentConnectedReason)
		checkAgentCondition(ctx, host.ID.String(), controllers.ValidatedCondition, controllers.ValidationsPassingReason)
	})

	It("deploy clusterDeployment with invalid machine cidr", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSNOSpec.Networking.MachineNetwork = []hiveext.MachineNetworkEntry{{CIDR: "1.2.3.5/24"}}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
	})

	It("deploy clusterDeployment without machine cidr", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSNOSpec.Networking.MachineNetwork = []hiveext.MachineNetworkEntry{}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
	})

	It("deploy clusterDeployment with invalid clusterImageSet", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSpec.ImageSetRef.Name = "invalid"
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.BackendErrorReason)
	})

	It("deploy clusterDeployment with missing clusterImageSet", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)

		// Create AgentClusterInstall without creating the clusterImageSet
		err := kubeClient.Create(ctx, &hiveext.AgentClusterInstall{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AgentClusterInstall",
				APIVersion: "hiveextension/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
			},
			Spec: *aciSpec,
		})
		Expect(err).To(BeNil())
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.BackendErrorReason)

		// Create ClusterImageSet
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.SyncedOkReason)
	})

	It("Delete clusterDeployment and validate deletion of ACI", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		Eventually(func() bool {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.ObjectMeta.OwnerReferences != nil
		}, "30s", "10s").Should(Equal(true))

		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKey)
		Expect(kubeClient.Delete(ctx, clusterDeploymentCRD)).ShouldNot(HaveOccurred())

		Eventually(func() bool {
			aci := &hiveext.AgentClusterInstall{}
			err := kubeClient.Get(ctx, installkey, aci)
			return apierrors.IsNotFound(err)
		}, "30s", "10s").Should(Equal(true))
	})

	It("deploy agentClusterInstall with manifest reference with bad manifest and then fixing it ", func() {
		By("Create cluster")
		ref := &corev1.LocalObjectReference{Name: "cluster-install-config"}
		aciSNOSpec.ManifestsConfigMapRef = ref
		content := `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 99-openshift-machineconfig-master-kargs
spec:
  kernelArguments:
    - 'loglevel=7'`

		By("Start installation without config map")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.BackendErrorReason)

		By("Deploy bad config map")
		data := map[string]string{"test.yaml": content, "test.dc": "test"}
		cm := deployOrUpdateConfigMap(ctx, kubeClient, ref.Name, data)
		defer func() {
			_ = kubeClient.Delete(ctx, cm)
		}()

		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterSpecSyncedCondition, controllers.InputErrorReason)

		By("Fixing configmap and expecting installation to start")
		// adding sleep to be sure all reconciles will finish, will test that requeue worked as expected
		time.Sleep(30 * time.Second)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterReadyReason)

		data = map[string]string{"test.yaml": content, "test2.yaml": content}
		deployOrUpdateConfigMap(ctx, kubeClient, ref.Name, data)
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterAlreadyInstallingReason)
	})

	It("delete agent and validate host deregistration", func() {
		By("Deploy SNO cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, controllers.ClusterRequirementsMetCondition, controllers.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		By("Validate no hosts currently belong to the cluster and clusterDeployment")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))

		Eventually(func() int {
			return len(getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout).Hosts)
		}, "2m", "2s").Should(Equal(0))

		By("Register a Host and validate that an agent CR was created")
		configureLocalAgentClient(cluster.ID.String())
		setupNewHost(ctx, "hostname1", *cluster.ID)
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(1))

		By("Validate that the backend reflects single host belong to the cluster")
		Eventually(func() int {
			return len(getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout).Hosts)
		}, "2m", "2s").Should(Equal(1))

		By("Delete agent CR and validate")
		agent := getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items[0]
		Expect(kubeClient.Delete(ctx, &agent)).To(BeNil())
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))

		By("Validate that the backend reflects that no hosts belong to the cluster")
		Eventually(func() int {
			return len(getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout).Hosts)
		}, "2m", "2s").Should(Equal(0))
	})
})

var _ = Describe("bmac reconcile flow", func() {
	if !Options.EnableKubeAPI {
		return
	}

	ctx := context.Background()

	var (
		bmh         *bmhv1alpha1.BareMetalHost
		bmhNsName   types.NamespacedName
		agentNsName types.NamespacedName
		infraNsName types.NamespacedName
	)

	BeforeEach(func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}

		infraNsName = types.NamespacedName{
			Name:      "infraenv",
			Namespace: Options.Namespace,
		}
		infraEnvSpec := getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())
		host := setupNewHost(ctx, "hostname1", *cluster.ID)
		agentNsName = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := bmhv1alpha1.BareMetalHostSpec{}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)
		bmhNsName = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
	})

	AfterEach(func() {
		cleanUP(ctx, kubeClient)
		clearDB()
	})

	Context("sno reconcile flow", func() {
		It("reconciles the infraenv", func() {
			bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, bmh)).ToNot(HaveOccurred())

			Eventually(func() bool {
				bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
				return bmh.Spec.Image != nil && bmh.Spec.Image.URL != ""
			}, "30s", "10s").Should(Equal(true))

			Expect(bmh.Spec.AutomatedCleaningMode).To(Equal(bmhv1alpha1.CleaningModeDisabled))
			Expect(bmh.ObjectMeta.Annotations).To(HaveKey(controllers.BMH_INSPECT_ANNOTATION))
			Expect(bmh.ObjectMeta.Annotations[controllers.BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
		})

		It("reconciles the agent", func() {
			bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
			bmh.Spec.BootMACAddress = getAgentMac(ctx, kubeClient, agentNsName)
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, bmh)).ToNot(HaveOccurred())

			Eventually(func() bool {
				bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
				return bmh.ObjectMeta.Annotations != nil && bmh.ObjectMeta.Annotations[controllers.BMH_HARDWARE_DETAILS_ANNOTATION] != ""
			}, "60s", "10s").Should(Equal(true))
		})
	})
})
