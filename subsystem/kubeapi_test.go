package subsystem

import (
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	agentv1 "github.com/openshift/hive/apis/hive/v1/agent"
	vspherev1 "github.com/openshift/hive/apis/hive/v1/vsphere"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fakeIgnitionConfigOverride           = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	badIgnitionConfigOverride            = `bad ignition config`
	clusterDeploymentNamePrefix          = "test-cluster"
	clusterAgentClusterInstallNamePrefix = "test-agent-cluster-install"
	doneStateInfo                        = "Done"
	clusterInstallStateInfo              = "Cluster is installed"
	clusterImageSetName                  = "openshift-v4.8.0"
)

var (
	imageSetsData = map[string]string{
		"openshift-v4.7.0":        "quay.io/openshift-release-dev/ocp-release:4.7.2-x86_64",
		"openshift-v4.8.0":        "quay.io/openshift-release-dev/ocp-release:4.8.0-fc.0-x86_64",
		"openshift-v4.9.0":        "quay.io/openshift-release-dev/ocp-release:4.9.11-x86_64",
		"openshift-v4.10.0":       "quay.io/openshift-release-dev/ocp-release:4.10.6-x86_64",
		"openshift-v4.10.0-arm":   "quay.io/openshift-release-dev/ocp-release:4.10.6-aarch64",
		"openshift-v4.11.0":       "quay.io/openshift-release-dev/ocp-release:4.11.0-x86_64",
		"openshift-v4.11.0-multi": "quay.io/openshift-release-dev/ocp-release:4.11.0-multi",
	}
)

func deployLocalObjectSecretIfNeeded(ctx context.Context, client k8sclient.Client) *corev1.LocalObjectReference {
	err := client.Get(
		ctx,
		types.NamespacedName{Namespace: Options.Namespace, Name: pullSecretName},
		&corev1.Secret{},
	)
	if apierrors.IsNotFound(err) {
		data := map[string]string{corev1.DockerConfigJsonKey: pullSecret}
		deploySecret(ctx, kubeClient, pullSecretName, data)
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

func updateAgentClusterInstallCRD(ctx context.Context, client k8sclient.Client, installkey types.NamespacedName, spec *hiveext.AgentClusterInstallSpec) {
	Eventually(func() error {
		agent := getAgentClusterInstallCRD(ctx, client, installkey)
		agent.Spec = *spec
		return kubeClient.Update(ctx, agent)
	}, "30s", "1s").Should(BeNil())
}

func deploySecret(ctx context.Context, client k8sclient.Client, secretName string, secretData map[string]string) {
	err := client.Create(ctx, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      secretName,
		},
		StringData: secretData,
	})
	Expect(err).To(BeNil())
}

func updateSecret(ctx context.Context, client k8sclient.Client, secretName string, secretData map[string]string) {
	err := client.Update(ctx, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      secretName,
		},
		StringData: secretData,
	})
	Expect(err).To(BeNil())
}

func deployAgentClusterInstallCRD(ctx context.Context, client k8sclient.Client, spec *hiveext.AgentClusterInstallSpec,
	clusterAgentClusterInstallName string) {
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
	GinkgoLogger(fmt.Sprintf("test '%s' creating cluster deployment '%s'", GinkgoT().Name(), spec.ClusterName))
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

func deployBMHCRD(ctx context.Context, client k8sclient.Client, name string, spec *metal3_v1alpha1.BareMetalHostSpec) {
	bmh := metal3_v1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BareMetalHost",
			APIVersion: "metal3.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
		},
		Spec: *spec,
		Status: metal3_v1alpha1.BareMetalHostStatus{
			Provisioning: metal3_v1alpha1.ProvisionStatus{State: metal3_v1alpha1.StateReady},
		},
	}

	err := client.Create(ctx, &bmh)
	Expect(err).To(BeNil())

	bmh.Status.Provisioning.State = metal3_v1alpha1.StateReady
	Expect(client.Status().Update(ctx, &bmh)).To(BeNil())
}

func deployPPICRD(ctx context.Context, client k8sclient.Client, name string, spec *metal3_v1alpha1.PreprovisioningImageSpec) {
	ppi := metal3_v1alpha1.PreprovisioningImage{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PreprovisioningImage",
			APIVersion: "metal3.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      name,
		},
		Spec:   *spec,
		Status: metal3_v1alpha1.PreprovisioningImageStatus{},
	}

	err := client.Create(ctx, &ppi)
	Expect(err).To(BeNil())
}

func addAnnotationToAgentClusterInstall(ctx context.Context, client k8sclient.Client, key types.NamespacedName, annotationKey string, annotationValue string) {
	Eventually(func() error {
		agentClusterInstallCRD := getAgentClusterInstallCRD(ctx, client, key)
		agentClusterInstallCRD.SetAnnotations(map[string]string{annotationKey: annotationValue})
		return kubeClient.Update(ctx, agentClusterInstallCRD)
	}, "30s", "10s").Should(BeNil())
}

func deployClusterImageSetCRD(ctx context.Context, client k8sclient.Client, imageSetRef *hivev1.ClusterImageSetReference) {
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

func getInfraEnvFromDBByKubeKey(ctx context.Context, db *gorm.DB, key types.NamespacedName, timeout int) *common.InfraEnv {
	var err error
	infraEnv := &common.InfraEnv{}
	start := time.Now()
	for time.Duration(timeout)*time.Second > time.Since(start) {
		infraEnv, err = common.GetInfraEnvFromDBWhere(db, "name = ? and kube_key_namespace = ?", key.Name, key.Namespace)
		if err == nil {
			return infraEnv
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			Expect(err).To(BeNil())
		}
		time.Sleep(time.Second)
	}
	Expect(err).To(BeNil())
	return infraEnv
}

func GetHostByKubeKey(ctx context.Context, db *gorm.DB, key types.NamespacedName, timeout int) *common.Host {
	var err error
	host := &common.Host{}
	start := time.Now()
	for time.Duration(timeout)*time.Second > time.Since(start) {
		host, err = common.GetHostFromDBWhere(db, "id = ? and kube_key_namespace = ?", key.Name, key.Namespace)
		if err == nil {
			return host
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			Expect(err).To(BeNil())
		}
		time.Sleep(time.Second)
	}
	Expect(err).To(BeNil())
	return host
}

func getClusterDeploymentAgents(ctx context.Context, client k8sclient.Client, clusterDeployment types.NamespacedName) *v1beta1.AgentList {
	agents := &v1beta1.AgentList{}
	clusterAgents := &v1beta1.AgentList{}
	err := client.List(ctx, agents)
	Expect(err).To(BeNil())
	clusterAgents.TypeMeta = agents.TypeMeta
	clusterAgents.ListMeta = agents.ListMeta
	for _, agent := range agents.Items {
		if agent.Spec.ClusterDeploymentName != nil &&
			agent.Spec.ClusterDeploymentName.Name == clusterDeployment.Name &&
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

func getClusterImageSetCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *hivev1.ClusterImageSet {
	clusterImageSet := &hivev1.ClusterImageSet{}
	err := client.Get(ctx, key, clusterImageSet)
	Expect(err).To(BeNil())
	return clusterImageSet
}

func getBmhCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *metal3_v1alpha1.BareMetalHost {
	bmh := &metal3_v1alpha1.BareMetalHost{}
	err := client.Get(ctx, key, bmh)
	Expect(err).To(BeNil())
	return bmh
}

func getPPICRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *metal3_v1alpha1.PreprovisioningImage {
	ppi := &metal3_v1alpha1.PreprovisioningImage{}
	err := client.Get(ctx, key, ppi)
	Expect(err).To(BeNil())
	return ppi
}

func getSecret(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *corev1.Secret {
	secret := &corev1.Secret{}
	err := client.Get(ctx, key, secret)
	Expect(err).To(BeNil())
	return secret
}

// configureLoclAgentClient reassigns the global agentBMClient variable to a client instance using local token auth
func configureLocalAgentClient(infraEnvID string) {
	if Options.AuthType != auth.TypeLocal {
		Fail(fmt.Sprintf("Agent client shouldn't be configured for local auth when auth type is %s", Options.AuthType))
	}

	key := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      "assisted-installer-local-auth-key",
	}
	secret := getSecret(context.Background(), kubeClient, key)
	privKeyPEM := secret.Data["ec-private-key.pem"]
	tok, err := gencrypto.LocalJWTForKey(infraEnvID, string(privKeyPEM), gencrypto.InfraEnvKey)
	Expect(err).To(BeNil())

	agentBMClient = client.New(clientcfg(auth.AgentAuthHeaderWriter(tok)))
}

func checkAgentCondition(ctx context.Context, hostId string, conditionType conditionsv1.ConditionType, reason string) {
	hostkey := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      hostId,
	}
	Eventually(func() string {
		condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, hostkey).Status.Conditions, conditionType)
		if condition != nil {
			return condition.Reason
		}
		return ""
	}, "3m", "20s").Should(Equal(reason))
}

func registerIPv6MasterNode(ctx context.Context, infraEnvID strfmt.UUID, name, ip string) *models.Host {
	host := &registerHost(infraEnvID).Host
	validHwInfoV6.Interfaces[0].IPV6Addresses = []string{ip}
	generateEssentialHostStepsWithInventory(ctx, host, name, validHwInfoV6)
	generateEssentialPrepareForInstallationSteps(ctx, host)
	//update role as master
	hostkey := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      host.ID.String(),
	}
	Eventually(func() error {
		agent := getAgentCRD(ctx, kubeClient, hostkey)
		agent.Spec.Role = models.HostRoleMaster
		return kubeClient.Update(ctx, agent)
	}, "120s", "30s").Should(BeNil())
	return host
}

func checkPlatformStatus(ctx context.Context, key types.NamespacedName, specPlatform, platform hiveext.PlatformType, umn *bool) {
	aci := getAgentClusterInstallCRD(ctx, kubeClient, key)
	ExpectWithOffset(1, aci.Spec.PlatformType).To(Equal(specPlatform))
	ExpectWithOffset(1, aci.Status.PlatformType).To(Equal(platform))
	ExpectWithOffset(1, aci.Status.UserManagedNetworking).To(Equal(umn))
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

func checkAgentClusterInstallConditionConsistency(ctx context.Context, key types.NamespacedName, conditionType string, reason string) {
	Consistently(func() string {
		condition := controllers.FindStatusCondition(getAgentClusterInstallCRD(ctx, kubeClient, key).Status.Conditions, conditionType)
		if condition != nil {
			return condition.Reason
		}
		return ""
	}, "15s", "5s").Should(Equal(reason))
}

func checkInfraEnvCondition(ctx context.Context, key types.NamespacedName, conditionType conditionsv1.ConditionType, message string) {
	Eventually(func() string {
		condition := conditionsv1.FindStatusCondition(getInfraEnvCRD(ctx, kubeClient, key).Status.Conditions, conditionType)
		if condition != nil {
			return condition.Message
		}
		return ""
	}, "2m", "1s").Should(ContainSubstring(message))
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
			NetworkType:    models.ClusterNetworkTypeOpenShiftSDN,
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  &hivev1.ClusterImageSetReference{Name: clusterImageSetName},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       0,
		},
		APIVIP:               "1.2.3.8",
		IngressVIP:           "1.2.3.9",
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeploymentName},
	}
}

func getDefaultNonePlatformAgentClusterInstallSpec(clusterDeploymentName string) *hiveext.AgentClusterInstallSpec {
	return &hiveext.AgentClusterInstallSpec{
		Networking: hiveext.Networking{
			MachineNetwork: []hiveext.MachineNetworkEntry{},
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "10.128.0.0/14",
				HostPrefix: 23,
			}},
			ServiceNetwork:        []string{"172.30.0.0/16"},
			NetworkType:           models.ClusterNetworkTypeOpenShiftSDN,
			UserManagedNetworking: swag.Bool(true),
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  &hivev1.ClusterImageSetReference{Name: clusterImageSetName},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       0,
		},
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
			NetworkType:    models.ClusterNetworkTypeOVNKubernetes,
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  &hivev1.ClusterImageSetReference{Name: clusterImageSetName},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 1,
			WorkerAgents:       0,
		},
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeploymentName},
	}
}

func getDefaultAgentClusterIPv6InstallSpec(clusterDeploymentName string) *hiveext.AgentClusterInstallSpec {
	return &hiveext.AgentClusterInstallSpec{
		Networking: hiveext.Networking{
			MachineNetwork: []hiveext.MachineNetworkEntry{{
				CIDR: "1001:db8::/120",
			}},
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "2002:db8::/53",
				HostPrefix: 64,
			}},
			ServiceNetwork: []string{"2003:db8::/112"},
			NetworkType:    models.ClusterNetworkTypeOVNKubernetes,
		},
		SSHPublicKey: sshPublicKey,
		ImageSetRef:  &hivev1.ClusterImageSetReference{Name: clusterImageSetName},
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       0,
		},
		APIVIP:               "1001:db8::64",
		IngressVIP:           "1001:db8::65",
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

func printCRs(ctx context.Context, client k8sclient.Client) {
	if GinkgoT().Failed() {
		var (
			multiErr                 *multierror.Error
			aciList                  hiveext.AgentClusterInstallList
			agentList                v1beta1.AgentList
			infraEnvList             v1beta1.InfraEnvList
			bareMetalHostList        metal3_v1alpha1.BareMetalHostList
			nmStateConfigList        v1beta1.NMStateConfigList
			classificationList       v1beta1.AgentClassificationList
			clusterImageSetList      hivev1.ClusterImageSetList
			clusterDeploymentList    hivev1.ClusterDeploymentList
			PreprovisioningImageList metal3_v1alpha1.PreprovisioningImageList
		)

		multiErr = multierror.Append(multiErr, client.List(ctx, &agentList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("Agent", agentList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &aciList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("AgentClusterInstall", aciList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &clusterDeploymentList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("ClusterDeployment", clusterDeploymentList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &clusterImageSetList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("ClusterImageSet", clusterImageSetList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &infraEnvList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("InfraEnv", infraEnvList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &nmStateConfigList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("NMStateConfig", nmStateConfigList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &classificationList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("AgentClassification", classificationList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &bareMetalHostList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("BareMetalHost", bareMetalHostList))

		multiErr = multierror.Append(multiErr, client.List(ctx, &PreprovisioningImageList, k8sclient.InNamespace(Options.Namespace)))
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger("PreprovisioningImage", PreprovisioningImageList))

		Expect(multiErr.ErrorOrNil()).To(BeNil())
	}
}

func cleanUpCRs(ctx context.Context, client k8sclient.Client) {
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &hivev1.ClusterDeployment{}, k8sclient.InNamespace(Options.Namespace)) // Should also delete all agents
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &hivev1.ClusterImageSet{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &v1beta1.InfraEnv{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &v1beta1.NMStateConfig{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &v1beta1.AgentClassification{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &metal3_v1alpha1.BareMetalHost{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())
	Eventually(func() error {
		return client.DeleteAllOf(ctx, &metal3_v1alpha1.PreprovisioningImage{}, k8sclient.InNamespace(Options.Namespace))
	}, "1m", "2s").Should(BeNil())

	// Check if tests pull secret exists and needs to be deleted
	secret := &corev1.Secret{
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

	psKey := types.NamespacedName{
		Namespace: Options.Namespace,
		Name:      pullSecretName,
	}

	err := kubeClient.Get(ctx, psKey, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		Eventually(func() error {
			return client.Delete(ctx, secret)
		}, "1m", "2s").Should(BeNil())
	}
}

func extractKernelArgs(internalInfraEnv *common.InfraEnv) (ret []v1beta1.KernelArgument) {
	ret = make([]v1beta1.KernelArgument, 0)
	if internalInfraEnv.KernelArguments == nil {
		return
	}
	var args models.KernelArguments
	Expect(json.Unmarshal([]byte(swag.StringValue(internalInfraEnv.KernelArguments)), &args)).ToNot(HaveOccurred())
	for _, arg := range args {
		ret = append(ret, v1beta1.KernelArgument{
			Operation: arg.Operation,
			Value:     arg.Value,
		})
	}
	return
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

	By("Verify AgentClassification Cleanup")
	Eventually(func() int {
		classificationList := &v1beta1.AgentClassificationList{}
		err := client.List(ctx, classificationList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(classificationList.Items)
	}, "2m", "2s").Should(Equal(0))

	By("Verify BareMetalHost Cleanup")
	Eventually(func() int {
		bareMetalHostList := &metal3_v1alpha1.BareMetalHostList{}
		err := client.List(ctx, bareMetalHostList, k8sclient.InNamespace(Options.Namespace))
		Expect(err).To(BeNil())
		return len(bareMetalHostList.Items)
	}, "2m", "2s").Should(Equal(0))
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
		aciSpecNonePlatform   *hiveext.AgentClusterInstallSpec
		aciSNOSpec            *hiveext.AgentClusterInstallSpec
		aciV6Spec             *hiveext.AgentClusterInstallSpec
		secretRef             *corev1.LocalObjectReference
	)

	BeforeEach(func() {
		secretRef = deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec = getDefaultClusterDeploymentSpec(secretRef)
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpecNonePlatform = getDefaultNonePlatformAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSNOSpec = getDefaultSNOAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciV6Spec = getDefaultAgentClusterIPv6InstallSpec(clusterDeploymentSpec.ClusterName)
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)

		infraNsName = types.NamespacedName{
			Name:      "infraenv" + randomNameSuffix(),
			Namespace: Options.Namespace,
		}
		infraEnvSpec = getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec)
	})

	It("Pull Secret validation error", func() {
		By("setting pull secret with wrong data")
		updateSecret(ctx, kubeClient, pullSecretName, map[string]string{
			corev1.DockerConfigJsonKey: WrongPullSecret})

		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		By("verify conditions")
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey,
			hiveext.ClusterSpecSyncedCondition,
			hiveext.ClusterBackendErrorReason)

		condition := controllers.FindStatusCondition(getAgentClusterInstallCRD(ctx, kubeClient, installkey).Status.Conditions,
			hiveext.ClusterSpecSyncedCondition)
		Expect(condition.Message).To(ContainSubstring("invalid pull secret data"))
	})

	It("Verify NetworkType configuration with IPv6", func() {
		By("Create cluster with network type OVN")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciV6Spec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		By("register hosts")
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv6Addresses(3, defaultCIDRv6)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerIPv6MasterNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		generateVerifyVipsPostStepReply(ctx, hosts[0], []string{aciV6Spec.APIVIP}, []string{aciV6Spec.IngressVIP}, models.VipVerificationSucceeded)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
		}

		By("verify validations are successfull")
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterValidatedCondition, hiveext.ClusterValidationsPassingReason)

		By("update network type to SDN")
		Eventually(func() error {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			aci.Spec.Networking.NetworkType = models.ClusterNetworkTypeOpenShiftSDN
			return kubeClient.Update(ctx, aci)
		}, "1m", "20s").Should(BeNil())

		By("verify validations are failing")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterValidatedCondition, hiveext.ClusterValidationsFailingReason)
		aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
		Expect(aci.Status.ValidationsInfo).ToNot(BeNil())
		condition := controllers.FindStatusCondition(aci.Status.Conditions, hiveext.ClusterValidatedCondition)
		if condition != nil {
			Expect(condition.Message).Should(ContainSubstring("use OVNKubernetes instead"))
		}
	})

	It("Verify NetworkType configuration with SNO", func() {
		snoSpec := getDefaultSNOAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		snoSpec.Networking.NetworkType = models.ClusterNetworkTypeOpenShiftSDN
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, snoSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		By("Spec Sync should fail with SDN Configuration")
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)

		By("Spec sync should succeed when applying OVN")
		Eventually(func() error {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			aci.Spec.Networking.NetworkType = models.ClusterNetworkTypeOVNKubernetes
			return kubeClient.Update(ctx, aci)
		}, "1m", "20s").Should(BeNil())
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)

		By("re-applying SDN should fail with spec sync")
		Eventually(func() error {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			aci.Spec.Networking.NetworkType = models.ClusterNetworkTypeOpenShiftSDN
			return kubeClient.Update(ctx, aci)
		}, "1m", "20s").Should(BeNil())
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)
	})

	It("deploy CD with ACI and agents - wait for ready, delete CD and verify ACI and agents deletion", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			Expect(agent.Status.ValidationsInfo).ToNot(BeNil())
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("verify default platform type status")
		checkPlatformStatus(ctx, installkey, "", hiveext.BareMetalPlatformType, swag.Bool(false))

		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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

	It("deploy nutanix platform", func() {
		aciSpec.PlatformType = hiveext.NutanixPlatformType
		imageSetRef4_11 := &hivev1.ClusterImageSetReference{
			Name: "openshift-v4.11.0",
		}
		aciSpec.ImageSetRef = imageSetRef4_11
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNodeWithInventory(ctx, *infraEnv.ID, hostname, ips[i], getDefaultNutanixInventory(ips[i]))
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			Expect(agent.Status.ValidationsInfo).ToNot(BeNil())
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("verify nutanix platform type status and spec")
		checkPlatformStatus(ctx, installkey, hiveext.NutanixPlatformType, hiveext.NutanixPlatformType, swag.Bool(false))

		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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

	It("deploy vsphere platform", func() {
		clusterDeploymentSpec.Platform = hivev1.Platform{VSphere: &vspherev1.Platform{}}
		aciSpec.PlatformType = hiveext.VSpherePlatformType
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNodeWithInventory(ctx, *infraEnv.ID, hostname, ips[i], getDefaultVmwareInventory(ips[i]))
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			Expect(agent.Status.ValidationsInfo).ToNot(BeNil())
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("verify vsphere platform type status and spec")
		checkPlatformStatus(ctx, installkey, hiveext.VSpherePlatformType, hiveext.VSpherePlatformType, swag.Bool(false))

		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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

	It("deploy None platform CD with ACI and agents - wait for ready, delete CD and verify ACI and agents deletion", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpecNonePlatform, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			Expect(agent.Status.ValidationsInfo).ToNot(BeNil())
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
			generateCommonDomainReply(ctx, h, clusterDeploymentSpec.ClusterName, clusterDeploymentSpec.BaseDomain)
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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

	It("deploy CD with ACI and agents with ignitionEndpoint", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		caCertificateSecretName := "ca-certificate"
		caCertificate := "abc"
		deploySecret(ctx, kubeClient, caCertificateSecretName, map[string]string{corev1.TLSCertKey: caCertificate})
		caSec := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Options.Namespace,
				Name:      caCertificateSecretName,
			},
		}
		defer func() {
			_ = kubeClient.Delete(ctx, caSec)
		}()
		aciSNOSpec.IgnitionEndpoint = &hiveext.IgnitionEndpoint{
			Url: "https://example.com",
			CaCertificateReference: &hiveext.CaCertificateReference{
				Namespace: Options.Namespace,
				Name:      caCertificateSecretName,
			},
		}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		b64Ca := b64.StdEncoding.EncodeToString([]byte(caCertificate))
		Eventually(func() bool {
			dbCluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			return dbCluster != nil && *dbCluster.IgnitionEndpoint.CaCertificate == b64Ca
		}, "1m", "10s").Should(BeTrue())
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		ignitionTokenSecretName := "ignition-token"
		ignitionEndpointToken := "abcdef"
		deploySecret(ctx, kubeClient, ignitionTokenSecretName, map[string]string{"ignition-token": ignitionEndpointToken})
		tokenSec := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Options.Namespace,
				Name:      ignitionTokenSecretName,
			},
		}
		defer func() {
			_ = kubeClient.Delete(ctx, tokenSec)
		}()
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)

		hostkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			agent.Spec.IgnitionEndpointTokenReference = &v1beta1.IgnitionEndpointTokenReference{
				Namespace: Options.Namespace,
				Name:      ignitionTokenSecretName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "1s").Should(BeNil())

		By("Verify Ignition Token in DB")
		Eventually(func() bool {
			dbHost := GetHostByKubeKey(ctx, db, hostkey, waitForReconcileTimeout)
			return dbHost != nil && dbHost.IgnitionEndpointToken == ignitionEndpointToken
		}, "30s", "1s").Should(BeTrue())
	})

	It("deploy CD with ACI and agents with node labels", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)

		hostkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		goodLabels := map[string]string{
			"first-label":      "first-value",
			"second-label":     "second-value",
			"label-with/slash": "blah",
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			agent.Spec.NodeLabels = goodLabels
			return kubeClient.Update(ctx, agent)
		}, "30s", "1s").ShouldNot(HaveOccurred())
		By("Verify node labels in DB")
		Eventually(func(g Gomega) {
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			g.Expect(agent.Spec.NodeLabels).To(Equal(goodLabels))
			g.Expect(funk.Find(agent.Status.Conditions, func(c conditionsv1.Condition) bool {
				return c.Type == v1beta1.SpecSyncedCondition && c.Status == corev1.ConditionTrue
			})).ToNot(BeNil())
			dbHost := GetHostByKubeKey(ctx, db, hostkey, waitForReconcileTimeout)
			var m map[string]string
			g.Expect(json.Unmarshal([]byte(dbHost.NodeLabels), &m)).ToNot(HaveOccurred())
			g.Expect(m).To(Equal(agent.Spec.NodeLabels))
		}, "30s", "1s").Should(Succeed())
		By("Try adding bad label")
		badLabels := map[string]string{
			"first-label":            "first-value",
			"second-label":           "second-value",
			"label-with/slash":       "blah",
			"label/with/two-slashes": "",
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			agent.Spec.NodeLabels = badLabels
			return kubeClient.Update(ctx, agent)
		}, "30s", "1s").ShouldNot(HaveOccurred())
		Eventually(func(g Gomega) {
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			g.Expect(agent.Spec.NodeLabels).To(Equal(badLabels))
			g.Expect(funk.Find(agent.Status.Conditions, func(c conditionsv1.Condition) bool {
				return c.Type == v1beta1.SpecSyncedCondition && c.Status == corev1.ConditionFalse
			})).ToNot(BeNil())
			dbHost := GetHostByKubeKey(ctx, db, hostkey, waitForReconcileTimeout)
			g.Expect(dbHost.NodeLabels).ToNot(BeEmpty())
			var m map[string]string
			g.Expect(json.Unmarshal([]byte(dbHost.NodeLabels), &m)).ToNot(HaveOccurred())
			g.Expect(m).To(Equal(goodLabels))
		}, "30s", "1s").Should(Succeed())
	})

	It("verify InfraEnv ISODownloadURL and image CreatedTime are not changing - update Annotations", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		By("Deploy InfraEnv")
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "15s", "5s").Should(Not(BeEmpty()))

		infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName)
		firstISO := infraEnv.Status.ISODownloadURL
		firstCreatedAt := infraEnv.Status.CreatedTime
		firstInitrd := infraEnv.Status.BootArtifacts.InitrdURL

		By("Update InfraEnv Annotations")
		Eventually(func() error {
			infraEnv.SetAnnotations(map[string]string{"foo": "bar"})
			return kubeClient.Update(ctx, infraEnv)
		}, "30s", "10s").Should(BeNil())

		By("Verify InfraEnv URLs do not change")
		Consistently(func() bool {
			status := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status
			return status.ISODownloadURL == firstISO && status.BootArtifacts.InitrdURL == firstInitrd
		}, "30s", "2s").Should(BeTrue())

		By("Verify InfraEnv Status CreatedTime has not changed")
		Expect(getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.CreatedTime).To(Equal(firstCreatedAt))
	})

	It("verify InfraEnv download URLs and image CreatedTime are changing  - update IgnitionConfigOverride", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		By("Deploy InfraEnv")
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "15s", "5s").Should(Not(BeEmpty()))

		infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName)
		firstISO := infraEnv.Status.ISODownloadURL
		firstCreatedAt := infraEnv.Status.CreatedTime
		firstInitrd := infraEnv.Status.BootArtifacts.InitrdURL

		By("Update InfraEnv IgnitionConfigOverride")
		Eventually(func() error {
			infraEnvSpec.IgnitionConfigOverride = fakeIgnitionConfigOverride
			infraEnv.Spec = *infraEnvSpec
			return kubeClient.Update(ctx, infraEnv)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() string {
			return getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKubeName, waitForReconcileTimeout).IgnitionConfigOverride
		}, "30s", "2s").Should(Equal(fakeIgnitionConfigOverride))

		By("Verify InfraEnv URLs have changed")
		Eventually(func() bool {
			status := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status
			return status.ISODownloadURL != firstISO && status.BootArtifacts.InitrdURL != firstInitrd
		}, "30s", "2s").Should(BeTrue())

		By("Verify InfraEnv Status CreatedTime has changed")
		Expect(getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.CreatedTime).ShouldNot(Equal(firstCreatedAt))
	})

	It("Create InfraEnv without ClusterDeployment and register Agent", func() {
		By("Deploy InfraEnv")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		By("Verify ISO URL is populated")
		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "15s", "1s").Should(Not(BeEmpty()))

		By("Verify infraEnv has no reference to CD")
		infraEnvCr := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName)
		Expect(infraEnvCr.Spec.ClusterRef).To(BeNil())

		By("Register Agent to InfraEnv")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Verify agent and host are not bound")
		h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.ClusterID).To(BeNil())
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "1s").Should(BeTrue())

		checkAgentCondition(ctx, host.ID.String(), v1beta1.BoundCondition, v1beta1.UnboundReason)
	})

	It("Agent labels", func() {
		By("Deploy InfraEnv")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		By("Verify ISO URL is populated")
		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "15s", "1s").Should(Not(BeEmpty()))

		By("Register Agent to InfraEnv")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Verify agent inventory labels")
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		var agentLabels map[string]string
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			agentLabels = agent.GetLabels()
			_, ok := agentLabels["inventory.agent-install.openshift.io/storage-hasnonrotationaldisk"]
			return ok
		}, "30s", "1s").Should(BeTrue())
		Expect(agentLabels["inventory.agent-install.openshift.io/storage-hasnonrotationaldisk"]).To(Equal("true"))
		Expect(agentLabels["inventory.agent-install.openshift.io/cpu-architecture"]).To(Equal("x86_64"))
		Expect(agentLabels["inventory.agent-install.openshift.io/cpu-virtenabled"]).To(Equal("false"))
		Expect(agentLabels["inventory.agent-install.openshift.io/host-manufacturer"]).To(Equal(validHwInfo.SystemVendor.Manufacturer))
		Expect(agentLabels["inventory.agent-install.openshift.io/host-productname"]).To(Equal(validHwInfo.SystemVendor.ProductName))
		Expect(agentLabels["inventory.agent-install.openshift.io/host-isvirtual"]).To(Equal(strconv.FormatBool(validHwInfo.SystemVendor.Virtual)))

		By("Verify agent classification labels")
		classificationXXL := v1beta1.AgentClassification{
			ObjectMeta: metav1.ObjectMeta{Name: "xxl", Namespace: Options.Namespace},
			Spec: v1beta1.AgentClassificationSpec{
				LabelKey:   "size",
				LabelValue: "xxl",
				Query:      fmt.Sprintf(".cpu.count == 16 and .memory.physicalBytes >= %d and .memory.physicalBytes < %d", int64(31*units.GiB), int64(33*units.GiB)),
			},
		}
		err := kubeClient.Create(ctx, &classificationXXL)
		Expect(err).To(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			agentLabels = agent.GetLabels()
			_, ok := agentLabels["agentclassification.agent-install.openshift.io/size"]
			return ok
		}, "30s", "1s").Should(BeTrue())
		Expect(agentLabels["agentclassification.agent-install.openshift.io/size"]).To(Equal("xxl"))
	})

	It("[kube-cpu-arch]create infra-env with arm64 cpu arch", func() {
		infraEnvSpec.ClusterRef = nil
		infraEnvSpec.CpuArchitecture = "arm64"
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKey).Status.ISODownloadURL
		}, "15s", "1s").Should(Not(BeEmpty()))

		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		Expect(infraEnv.CPUArchitecture).To(Equal("arm64"))
	})

	It("[kube-cpu-arch]mismatch cpu architecture between infra-env and bound cluster", func() {
		By("deploy cluster with openshiftVersion 4.10 and x86_64")
		imageSetRef4_10 := &hivev1.ClusterImageSetReference{
			Name: "openshift-v4.10.0",
		}
		aciSpec.ImageSetRef = imageSetRef4_10
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		By("deploy infraenv with a reference to openshiftVersion 4.10 cluster and arm64")
		infraEnvSpec.CpuArchitecture = "arm64"
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition,
			"Specified CPU architecture (arm64) doesn't match the cluster (x86_64)")
	})

	It("[multiarch] Create multiarch cluster and bind infraenvs", func() {
		By("deploy cluster with openshiftVersion 4.11 and multiarch")
		imageSetRef4_11 := &hivev1.ClusterImageSetReference{
			Name: "openshift-v4.11.0-multi",
		}
		aciSpec.ImageSetRef = imageSetRef4_11
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.CPUArchitecture).Should(Equal("multi"))
		Expect(cluster.OcpReleaseImage).Should(Equal("quay.io/openshift-release-dev/ocp-release:4.11.0-multi"))

		By("deploy infraenv with arm64 architecure")
		infraEnvSpec.CpuArchitecture = "arm64"
		infraEnvArm := types.NamespacedName{
			Name:      "infraenv" + randomNameSuffix(),
			Namespace: Options.Namespace,
		}
		deployInfraEnvCRD(ctx, kubeClient, infraEnvArm.Name, infraEnvSpec)

		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvArm).Status.ISODownloadURL
		}, "15s", "1s").Should(Not(BeEmpty()))

		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvArm, waitForReconcileTimeout)
		Expect(infraEnv.CPUArchitecture).To(Equal("arm64"))

		By("deploy infraenv with x86 architecture")
		infraEnvSpec.CpuArchitecture = "x86_64"
		infraEnvX86 := types.NamespacedName{
			Name:      "infraenv" + randomNameSuffix(),
			Namespace: Options.Namespace,
		}
		deployInfraEnvCRD(ctx, kubeClient, infraEnvX86.Name, infraEnvSpec)

		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvX86).Status.ISODownloadURL
		}, "15s", "1s").Should(Not(BeEmpty()))

		infraEnv2 := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvX86, waitForReconcileTimeout)
		Expect(infraEnv2.CPUArchitecture).To(Equal("x86_64"))

		By("fail to deploy infraenv with fake architecture")
		infraEnvSpec.CpuArchitecture = "fake"
		infraEnvFake := types.NamespacedName{
			Name:      "infraenv" + randomNameSuffix(),
			Namespace: Options.Namespace,
		}
		deployInfraEnvCRD(ctx, kubeClient, infraEnvFake.Name, infraEnvSpec)

		checkInfraEnvCondition(ctx, infraEnvFake, v1beta1.ImageCreatedCondition,
			"does not have a matching OpenShift release image")
	})

	It("deploy CD with ACI and agents - wait for ready, delete ACI only and verify agents deletion", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Verify ClusterDeployment ReadyForInstallation")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
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
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			return h.RequestedHostname
		}, "2m", "10s").Should(Equal("newhostname"))
		Eventually(func() string {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			return h.InstallationDiskID
		}, "2m", "10s").Should(Equal(sdb.ID))
		Eventually(func() string {
			return getAgentCRD(ctx, kubeClient, key).Status.InstallationDiskID
		}, "2m", "10s").Should(Equal(sdb.ID))
		Eventually(func() string {
			return getAgentCRD(ctx, kubeClient, key).Spec.InstallationDiskID
		}, "2m", "10s").Should(Equal(sdb.ID))
		Eventually(func() bool {
			return conditionsv1.IsStatusConditionTrue(getAgentCRD(ctx, kubeClient, key).Status.Conditions, v1beta1.SpecSyncedCondition)
		}, "2m", "10s").Should(Equal(true))
		Eventually(func() string {
			return getAgentCRD(ctx, kubeClient, key).Status.Inventory.SystemVendor.Manufacturer
		}, "2m", "10s").Should(Equal(validHwInfo.SystemVendor.Manufacturer))
	})

	It("deploy clusterDeployment with agent,bmh and ignition config override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := metal3_v1alpha1.BareMetalHostSpec{BootMACAddress: getAgentMac(ctx, kubeClient, key)}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_IGNITION_CONFIG_OVERRIDES: fakeIgnitionConfigOverride})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
		h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())

			return len(h.IgnitionConfigOverrides)
		}, "2m", "10s").Should(Equal(0))
	})

	It("deploy clusterDeployment with agent,bmh and node labels", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := metal3_v1alpha1.BareMetalHostSpec{BootMACAddress: getAgentMac(ctx, kubeClient, key)}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(map[string]string{controllers.NODE_LABEL_PREFIX + "my-label": "blah"})
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			if h.NodeLabels == "" {
				return false
			}
			var m map[string]string
			Expect(json.Unmarshal([]byte(h.NodeLabels), &m)).ToNot(HaveOccurred())
			Expect(m).To(HaveLen(1))
			Expect(m["my-label"]).To(Equal("blah"))
			return true
		}, "2m", "10s").Should(Equal(true))

		By("Clean node labels")
		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetAnnotations(make(map[string]string))
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			if h.NodeLabels == "" {
				return true
			}
			var m map[string]string
			Expect(json.Unmarshal([]byte(h.NodeLabels), &m)).ToNot(HaveOccurred())
			return len(m) == 0
		}, "2m", "10s").Should(Equal(true))
	})

	It("deploy clusterDeployment with agent and invalid ignition config", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			agent := getAgentCRD(ctx, kubeClient, hostkey)
			Expect(agent.Status.ValidationsInfo).ToNot(BeNil())
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
		}

		By("Invalid ignition config - invalid json")
		for _, host := range hosts {
			hostkey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}

			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			Expect(h.IgnitionConfigOverrides).To(BeEmpty())

			Eventually(func() error {
				agent := getAgentCRD(ctx, kubeClient, hostkey)
				agent.Spec.IgnitionConfigOverrides = badIgnitionConfigOverride
				return kubeClient.Update(ctx, agent)
			}, "30s", "10s").Should(BeNil())

			Eventually(func() bool {
				condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, hostkey).Status.Conditions, v1beta1.SpecSyncedCondition)
				if condition != nil {
					return strings.Contains(condition.Message, "error parsing ignition: config is not valid")
				}
				return false
			}, "15s", "2s").Should(Equal(true))

			h, err = common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
			Expect(err).To(BeNil())
			Expect(h.IgnitionConfigOverrides).To(BeEmpty())
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

		By("Verify cluster condition UnsyncedAgent")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterUnsyncedAgentsReason)
	})

	It("deploy clusterDeployment with agent and update installer args", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
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
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
		h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).NotTo(BeEmpty())

		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.InstallerArgs = ""
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() int {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
			condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, key).Status.Conditions, v1beta1.SpecSyncedCondition)
			if condition != nil {
				return strings.Contains(condition.Message, "Fail to unmarshal installer args")
			}
			return false
		}, "15s", "2s").Should(Equal(true))
		h, err = common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
			condition := conditionsv1.FindStatusCondition(getAgentCRD(ctx, kubeClient, key).Status.Conditions, v1beta1.SpecSyncedCondition)
			if condition != nil {
				return strings.Contains(condition.Message, "found unexpected flag --wrong-param")
			}
			return false
		}, "15s", "2s").Should(Equal(true))
		h, err = common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
		Expect(err).To(BeNil())
		Expect(h.InstallerArgs).To(BeEmpty())
	})

	It("deploy clusterDeployment with agent,bmh and installer args", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		image := &metal3_v1alpha1.Image{URL: "http://buzz.lightyear.io/discovery-image.iso"}
		bmhSpec := metal3_v1alpha1.BareMetalHostSpec{BootMACAddress: getAgentMac(ctx, kubeClient, key), Image: image}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)

		installerArgs := `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`

		Eventually(func() error {
			bmh := getBmhCRD(ctx, kubeClient, key)
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			bmh.SetAnnotations(map[string]string{controllers.BMH_AGENT_INSTALLER_ARGS: installerArgs})
			return kubeClient.Update(ctx, bmh)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
		h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
			h, err := common.GetHostFromDB(db, infraEnv.ID.String(), host.ID.String())
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.NoProxy).Should(Equal(""))
		Expect(cluster.HTTPProxy).Should(Equal(""))
		Expect(cluster.HTTPSProxy).Should(Equal(""))
		Expect(cluster.AdditionalNtpSource).Should(Equal(""))
		Expect(cluster.Hyperthreading).Should(Equal("all"))
		Expect(cluster.CPUArchitecture).Should(Equal("x86_64"))

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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		Expect(infraEnv.Generated).Should(Equal(true))
		By("Validate proxy settings.", func() {
			Expect(swag.StringValue(infraEnv.Proxy.NoProxy)).Should(Equal("192.168.1.1"))
			Expect(swag.StringValue(infraEnv.Proxy.HTTPProxy)).Should(Equal("http://192.168.1.2"))
			Expect(swag.StringValue(infraEnv.Proxy.HTTPSProxy)).Should(Equal("http://192.168.1.3"))
		})
		By("Validate additional NTP settings.")
		Expect(infraEnv.AdditionalNtpSources).Should(ContainSubstring("192.168.1.4"))
		By("InfraEnv image type defaults to minimal-iso.")
		Expect(common.ImageTypeValue(infraEnv.Type)).Should(Equal(models.ImageTypeMinimalIso))
	})

	It("[kube-cpu-arch]deploy ClusterDeployment with arm64 architecture", func() {
		//Note: arm is supported with user managed networking only
		armImageSetRef := &hivev1.ClusterImageSetReference{Name: "openshift-v4.10.0-arm"}
		aciSNOSpec.ImageSetRef = armImageSetRef
		deployClusterImageSetCRD(ctx, kubeClient, armImageSetRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.CPUArchitecture).Should(Equal("arm64"))
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
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)

		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		configureLocalAgentClient(cluster.ID.String())

		infraEnvSpec.IgnitionConfigOverride = fakeIgnitionConfigOverride

		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKubeName, waitForReconcileTimeout)
		Expect(infraEnv.IgnitionConfigOverride).Should(Equal(fakeIgnitionConfigOverride))
		Expect(infraEnv.Generated).Should(Equal(true))
	})

	It("deploy clusterDeployment full install with infraenv in different namespace", func() {
		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		data := map[string]string{corev1.DockerConfigJsonKey: pullSecret}
		s := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      pullSecretName,
			},
			StringData: data,
			Type:       corev1.SecretTypeDockerConfigJson,
		}
		Expect(kubeClient.Create(ctx, s)).To(BeNil())

		// Deploy InfraEnv in default namespace
		infraEnvKey := types.NamespacedName{
			Namespace: "default",
			Name:      infraNsName.Name,
		}
		err := kubeClient.Create(ctx, &v1beta1.InfraEnv{
			TypeMeta: metav1.TypeMeta{
				Kind:       "InfraEnv",
				APIVersion: getAPIVersion(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      infraNsName.Name,
			},
			Spec: *infraEnvSpec,
		})
		Expect(err).To(BeNil())
		defer func() {
			Expect(kubeClient.DeleteAllOf(ctx, &v1beta1.InfraEnv{}, k8sclient.InNamespace("default"))).To(BeNil())
			Expect(kubeClient.Delete(ctx, s)).To(BeNil())
		}()

		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)

		By("Check InfraEnv Event URL exists")
		Eventually(func() string {
			infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
			return infraEnv.Status.InfraEnvDebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*infra_env_id=%s", infraEnv.ID.String())))

		infraEnvCr := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
		_, err = testEventUrl(infraEnvCr.Status.InfraEnvDebugInfo.EventsURL)
		Expect(err).To(BeNil())

		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		generateFullMeshConnectivity(ctx, ips[0], host)

		key := types.NamespacedName{
			Namespace: "default",
			Name:      host.ID.String(),
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}

		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Check that Agent Event URL is valid")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*host_id=%s", host.ID.String())))

		agent := getAgentCRD(ctx, kubeClient, key)
		_, err = testEventUrl(agent.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Wait for installing")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstallationInProgressReason)

		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		updateProgress(*host.ID, *infraEnv.ID, models.HostStageDone)

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err = agentBMClient.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verify Cluster Metadata")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterFailedCondition, hiveext.ClusterNotFailedReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterStoppedCondition, hiveext.ClusterStoppedCompletedReason)
	})

	It("deploy infraEnv before clusterDeployment", func() {
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)

		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		Expect(infraEnv.Generated).Should(Equal(true))
	})

	It("deploy infraEnv in default namespace before clusterDeployment", func() {
		// Deploy Pull Secret in default namespace
		data := map[string]string{corev1.DockerConfigJsonKey: pullSecret}
		s := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      pullSecretName,
			},
			StringData: data,
			Type:       corev1.SecretTypeDockerConfigJson,
		}
		Expect(kubeClient.Create(ctx, s)).To(BeNil())

		// Deploy InfraEnv in default namespace
		err := kubeClient.Create(ctx, &v1beta1.InfraEnv{
			TypeMeta: metav1.TypeMeta{
				Kind:       "InfraEnv",
				APIVersion: getAPIVersion(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      infraNsName.Name,
			},
			Spec: *infraEnvSpec,
		})
		Expect(err).To(BeNil())
		defer func() {
			Expect(kubeClient.DeleteAllOf(ctx, &v1beta1.InfraEnv{}, k8sclient.InNamespace("default"))).To(BeNil())
			Expect(kubeClient.Delete(ctx, s)).To(BeNil())
		}()

		infraEnvKubeName := types.NamespacedName{
			Namespace: "default",
			Name:      infraNsName.Name,
		}
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)

		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKubeName, waitForReconcileTimeout)
		Expect(infraEnv.Generated).Should(Equal(true))
	})

	It("deploy clusterDeployment and infraEnv and with an invalid ignition override", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)

		infraEnvSpec.IgnitionConfigOverride = badIgnitionConfigOverride
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateFailedToCreate+": error parsing ignition: config is not valid")
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
			Name:           hiveext.MasterAgentMachinePool,
			Hyperthreading: hiveext.HyperthreadingDisabled,
		}
		aciSpec.Compute = []hiveext.AgentMachinePool{
			{Name: hiveext.WorkerAgentMachinePool, Hyperthreading: hiveext.HyperthreadingDisabled},
		}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingNone)

		By("update deployment with hyperthreading enabled on master only")
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.ControlPlane = &hiveext.AgentMachinePool{
			Name:           hiveext.MasterAgentMachinePool,
			Hyperthreading: hiveext.HyperthreadingEnabled,
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingMasters)

		By("update deployment with hyperthreading enabled on workers only")
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.Compute = []hiveext.AgentMachinePool{
			{Name: hiveext.WorkerAgentMachinePool, Hyperthreading: hiveext.HyperthreadingEnabled},
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyHyperthreadingSetup(models.ClusterHyperthreadingWorkers)
	})

	It("deploy clusterDeployment with disk encryption configuration", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		verifyDiskEncryptionConfig := func(enableOn *string, mode *string, tangServers string) {
			Eventually(func() bool {
				c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
				return swag.StringValue(c.DiskEncryption.EnableOn) == swag.StringValue(enableOn) &&
					swag.StringValue(c.DiskEncryption.Mode) == swag.StringValue(mode) &&
					c.DiskEncryption.TangServers == tangServers
			}, "1m", "10s").Should(BeTrue())
		}
		By("new deployment with disk encryption disabled")
		aciSpec.DiskEncryption = &hiveext.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
		}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyDiskEncryptionConfig(swag.String(models.DiskEncryptionEnableOnNone), nil, "")

		By("update deployment with disk encryption enabled with tpmv2 on master only")
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.DiskEncryption = &hiveext.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyDiskEncryptionConfig(swag.String(models.DiskEncryptionEnableOnMasters), swag.String(models.DiskEncryptionModeTpmv2), "")

		By("update deployment with disk encryption enabled with tang on workers only")
		tangServersConfig := `[{"URL":"http://tang.example.com:7500","Thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.DiskEncryption = &hiveext.DiskEncryption{
			EnableOn:    swag.String(models.DiskEncryptionEnableOnWorkers),
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: tangServersConfig,
		}
		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		verifyDiskEncryptionConfig(swag.String(models.DiskEncryptionEnableOnWorkers), swag.String(models.DiskEncryptionModeTang), tangServersConfig)
	})

	It("deploy clusterDeployment with Proxy", func() {
		httpProxy := "http://proxy.org"
		httpsProxy := "http://secureproxy.org"
		noProxy := "acme.com"
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}

		By("new deployment with proxy")
		aciSpec.Proxy = &hiveext.Proxy{HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: noProxy}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)

		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
			return c.HTTPProxy == httpProxy &&
				c.HTTPSProxy == httpsProxy &&
				c.NoProxy == noProxy
		}, "1m", "10s").Should(BeTrue())

		By("update deployment with new proxy values")
		httpProxy = "http://proxy2.org"
		httpsProxy = "http://secureproxy2.org"
		noProxy = "acme2.com"
		aciSpec = getDefaultAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
		aciSpec.Proxy = &hiveext.Proxy{HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: noProxy}

		updateAgentClusterInstallCRD(ctx, kubeClient, installkey, aciSpec)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
			return c.HTTPProxy == httpProxy &&
				c.HTTPSProxy == httpsProxy &&
				c.NoProxy == noProxy
		}, "1m", "10s").Should(BeTrue())
	})

	It("fail to deploy clusterDeployment with NoProxy wildcard - OpenshiftVersion does not support NoProxy wildcard", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}

		By("new deployment with NoProxy")
		aciSpec.Proxy = &hiveext.Proxy{HTTPProxy: "", HTTPSProxy: "", NoProxy: "*"}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)
	})

	It("deploy clusterDeployment with NoProxy wildcard - OpenshiftVersion does support NoProxy wildcard", func() {
		noProxy := "*"
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		clusterKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}

		By("new deployment with NoProxy")
		aciSpec.Proxy = &hiveext.Proxy{HTTPProxy: "", HTTPSProxy: "", NoProxy: "*"}
		imageSetRef4_11 := &hivev1.ClusterImageSetReference{
			Name: "openshift-v4.11.0",
		}
		aciSpec.ImageSetRef = imageSetRef4_11
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)

		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
			return c.NoProxy == noProxy
		}, "1m", "10s").Should(BeTrue())

		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.CPUArchitecture).Should(Equal(common.X86CPUArchitecture))
		Expect(cluster.OcpReleaseImage).Should(Equal("quay.io/openshift-release-dev/ocp-release:4.11.0-x86_64"))
	})

	It("deploy infraEnv with NoProxy wildcard", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		infraEnvSpec.Proxy = &v1beta1.Proxy{
			NoProxy: "*",
		}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		Expect(infraEnv.Generated).Should(Equal(true))
		By("Validate NoProxy settings.", func() {
			Expect(swag.StringValue(infraEnv.Proxy.NoProxy)).Should(Equal("*"))
		})
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKubeName, waitForReconcileTimeout)
		Expect(cluster.InstallConfigOverrides).Should(Equal(""))

		installConfigOverrides := `{"controlPlane": "malformed json": "Enabled"}}`
		addAnnotationToAgentClusterInstall(ctx, kubeClient, installkey, controllers.InstallConfigOverrides, installConfigOverrides)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		infraEnvSpec.NMStateConfigLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, v1beta1.ImageStateCreated)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		var staticNetworkConfig []*models.HostStaticNetworkConfig
		Expect(json.Unmarshal([]byte(infraEnv.StaticNetworkConfig), &staticNetworkConfig)).ToNot(HaveOccurred())
		Expect(staticNetworkConfig).To(HaveLen(1))
		Expect(staticNetworkConfig[0].NetworkYaml).To(Equal(hostStaticNetworkConfig.NetworkYaml))
		Expect(infraEnv.Generated).Should(Equal(true))
	})

	It("Bind Agent to not existing ClusterDeployment", func() {
		By("Deploy InfraEnv without cluster reference")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		By("Verify ISO URL is populated")
		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "15s", "5s").Should(Not(BeEmpty()))

		By("Verify infraEnv has no reference to CD")
		infraEnvCr := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName)
		Expect(infraEnvCr.Spec.ClusterRef).To(BeNil())

		By("Register Agent to InfraEnv")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Verify agent is not bind")
		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "10s").Should(BeTrue())

		By("Wait for Agent to be Known Unbound")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusKnownUnbound
		}, "1m", "10s").Should(BeTrue())

		By("Bind Agent to invalid CD")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      "ghostcd",
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		checkAgentCondition(ctx, host.ID.String(), v1beta1.SpecSyncedCondition, v1beta1.BackendErrorReason)
	})

	It("deploy clusterDeployment and infraEnv and with an invalid NMState config YAML", func() {
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
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		infraEnvSpec.NMStateConfigLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}}
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		// InfraEnv Reconcile takes longer, since it needs to generate the image.
		checkInfraEnvCondition(ctx, infraEnvKubeName, v1beta1.ImageCreatedCondition, "Unsupported keys found")
	})

	It("Unbind", func() {
		By("Create InfraEnv - pool")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		By("Register host to pool")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		By("Bind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID != nil
		}, "30s", "1s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Unbind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = nil
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID == nil
		}, "30s", "1s").Should(BeTrue())

		By("Check Agent State - unbinding")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusUnbinding
		}, "1m", "10s").Should(BeTrue())
	})

	It("Agent back to InfraEnv on CD delete", func() {
		By("Create InfraEnv - pool")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		By("Register host to pool")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		By("Bind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID != nil
		}, "30s", "1s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Delete ClusterDeployment")
		Expect(kubeClient.Delete(ctx, getClusterDeploymentCRD(ctx, kubeClient, clusterKey))).ShouldNot(HaveOccurred())

		By("Check that host is not bound to cluster")
		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID == nil
		}, "30s", "1s").Should(BeTrue())

		By("Check that Agent is unbind")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "1s").Should(BeTrue())
	})

	It("Move agent to different CD Unbind/bind", func() {
		By("Create InfraEnv - pool")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		By("Register host to pool")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)

		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Create source CD")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		By("Bind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID != nil
		}, "30s", "1s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Create target CD")
		targetCDSpec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, targetCDSpec)
		targetAciSNOSpec := getDefaultSNOAgentClusterInstallSpec(targetCDSpec.ClusterName)
		deployAgentClusterInstallCRD(ctx, kubeClient, targetAciSNOSpec, targetCDSpec.ClusterInstallRef.Name)

		targetClusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      targetCDSpec.ClusterName,
		}
		getClusterFromDB(ctx, kubeClient, db, targetClusterKey, waitForReconcileTimeout)

		By("Change Agent CD ref")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      targetCDSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Wait for Agent State - unbinding")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusUnbinding
		}, "1m", "10s").Should(BeTrue())

		By("Register to pool again")
		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateFullMeshConnectivity(ctx, ips[0], host)
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Wait for Agent State - binding")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusBinding
		}, "1m", "10s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, targetCDSpec.ClusterName, "hive.example.com")

		By("Wait for Agent State - Known")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusKnown
		}, "1m", "10s").Should(BeTrue())
	})

	Context("kernel arguments", func() {
		expectedSpecKargs := func(k []v1beta1.KernelArgument) []v1beta1.KernelArgument {
			if k == nil {
				return make([]v1beta1.KernelArgument, 0)
			}
			return k
		}

		It("Create infraenv with kernel arguments", func() {
			infraEnvSpec.ClusterRef = nil
			kargs := []v1beta1.KernelArgument{
				{
					Operation: "append",
					Value:     "p1",
				},
				{
					Operation: "append",
					Value:     `p2="this is an argument"`,
				},
			}
			infraEnvSpec.KernelArguments = kargs
			infraEnvKey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      infraNsName.Name,
			}
			deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
			infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
			Expect(extractKernelArgs(infraEnv)).To(Equal(kargs))
		})

		setAndCheck := func(kargs []v1beta1.KernelArgument) {
			infraEnvKey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      infraNsName.Name,
			}

			Eventually(func() error {
				infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
				infraEnv.Spec.KernelArguments = kargs
				return kubeClient.Update(ctx, infraEnv)
			}, "10s", "1s").ShouldNot(HaveOccurred())
			Eventually(func(g Gomega) {
				infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
				g.Expect(expectedSpecKargs(infraEnv.Spec.KernelArguments)).To(Equal(kargs))
				dbInfraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, 1)
				g.Expect(extractKernelArgs(dbInfraEnv)).To(Equal(kargs))
			}, "30s", "1s").Should(Succeed())
		}

		It("Update kernel arguments", func() {
			By("Register infra env")
			infraEnvSpec.ClusterRef = nil
			deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

			infraEnvKey := types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      infraNsName.Name,
			}
			dbInfraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
			Expect(dbInfraEnv.KernelArguments).To(BeNil())
			infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
			Expect(infraEnv).ToNot(BeNil())

			By("Set with kernel arguments")
			kargs1 := []v1beta1.KernelArgument{
				{
					Operation: "append",
					Value:     "p1",
				},
				{
					Operation: "append",
					Value:     `p2="this is an argument"`,
				},
			}
			setAndCheck(kargs1)

			By("Set with different kernel arguments")
			kargs2 := []v1beta1.KernelArgument{
				{
					Operation: "append",
					Value:     "p3",
				},
				{
					Operation: "append",
					Value:     `p4="this is another argument"`,
				},
			}
			setAndCheck(kargs2)

			By("Clear kernel arguments")
			setAndCheck(make([]v1beta1.KernelArgument, 0))
		})
		DescribeTable("unsupported cases",
			func(operation, value string) {
				infraEnvSpec.ClusterRef = nil
				infraEnvKey := types.NamespacedName{
					Namespace: Options.Namespace,
					Name:      infraNsName.Name,
				}
				deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
				infraEnv := getInfraEnvCRD(ctx, kubeClient, infraEnvKey)
				infraEnv.Spec.KernelArguments = []v1beta1.KernelArgument{
					{
						Operation: operation,
						Value:     value,
					},
				}
				Expect(kubeClient.Update(ctx, infraEnv)).To(HaveOccurred())
			},
			Entry("illegal operation", "illegal", "p1"),
			Entry("illegal value", "append", "value with unquoted spaces"),
		)
	})

	It("Delete infraenv with bound agents", func() {
		By("Create InfraEnv - pool")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		By("Register host to pool")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		By("Bind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID != nil
		}, "30s", "1s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateFullMeshConnectivity(ctx, ips[0], host)
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Wait for Agent to be Known Bound")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusKnown
		}, "1m", "10s").Should(BeTrue())

		By("Delete InfraEnv")
		Expect(kubeClient.Delete(ctx, getInfraEnvCRD(ctx, kubeClient, infraEnvKey))).ShouldNot(HaveOccurred())

		By("Verify InfraEnv not deleted")
		Consistently(func() error {
			infraEnv := &v1beta1.InfraEnv{}
			return kubeClient.Get(ctx, infraEnvKey, infraEnv)
		}, "30s", "2s").Should(BeNil())

		By("Delete ClusterDeployment")
		Expect(kubeClient.Delete(ctx, getClusterDeploymentCRD(ctx, kubeClient, clusterKey))).ShouldNot(HaveOccurred())

		By("Verify InfraEnv deleted")
		Eventually(func() bool {
			infraEnv := &v1beta1.InfraEnv{}
			err := kubeClient.Get(ctx, infraEnvKey, infraEnv)
			return apierrors.IsNotFound(err)
		}, "1m", "10s").Should(BeTrue())

	})

	It("Bind Agent from Infraenv and install SNO", func() {
		By("Deploy InfraEnv without cluster reference")
		infraEnvSpec.ClusterRef = nil
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKubeName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}

		By("Verify ISO URL is populated")
		Eventually(func() string {
			return getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName).Status.ISODownloadURL
		}, "30s", "5s").Should(Not(BeEmpty()))

		By("Verify infraEnv has no reference to CD")
		infraEnvCr := getInfraEnvCRD(ctx, kubeClient, infraEnvKubeName)
		Expect(infraEnvCr.Spec.ClusterRef).To(BeNil())

		By("Register Agent to InfraEnv")
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := &registerHost(*infraEnv.ID).Host
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		hwInfo := validHwInfo
		hwInfo.Interfaces[0].IPV4Addresses = []string{defaultCIDRv4}
		generateFullMeshConnectivity(ctx, ips[0], host)
		generateHWPostStepReply(ctx, host, hwInfo, "hostname1")

		By("Verify agent is not bind")
		hostKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "10s").Should(BeTrue())

		By("Wait for Agent to be Known Unbound")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Status.DebugInfo.State == models.HostStatusKnownUnbound
		}, "1m", "10s").Should(BeTrue())

		By("Create SNO cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		By("Check ACI condition ValidationsFailing")
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterValidatedCondition, hiveext.ClusterValidationsFailingReason)

		By("Bind Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
				Namespace: Options.Namespace,
				Name:      clusterDeploymentSpec.ClusterName,
			}
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName != nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID != nil
		}, "30s", "1s").Should(BeTrue())

		registerHostByUUID(host.InfraEnvID, *host.ID)
		generateEssentialHostSteps(ctx, host, "hostname1", defaultCIDRv4)
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
		generateEssentialPrepareForInstallationSteps(ctx, host)

		By("Check ACI condition UnapprovedAgentsReason")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterUnapprovedAgentsReason)

		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Wait for installing")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstallationInProgressReason)

		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		checkAgentCondition(ctx, host.ID.String(), v1beta1.InstalledCondition, v1beta1.InstallationInProgressReason)

		updateProgress(*host.ID, *infraEnv.ID, models.HostStageDone)

		By("Complete Installation")
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)
		checkAgentCondition(ctx, host.ID.String(), v1beta1.InstalledCondition, v1beta1.InstalledReason)

		By("Verify cluster installed")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)

		By("Delete Cluster Deployment")
		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKey)
		Expect(kubeClient.Delete(ctx, clusterDeploymentCRD)).ShouldNot(HaveOccurred())

		By("Check Agent is back to infraenv - unbinding-pending-user-action")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, hostKey)
			return agent.Spec.ClusterDeploymentName == nil
		}, "30s", "1s").Should(BeTrue())

		Eventually(func() bool {
			agent := GetHostByKubeKey(ctx, db, hostKey, waitForReconcileTimeout)
			return agent.ClusterID == nil
		}, "30s", "1s").Should(BeTrue())

		checkAgentCondition(ctx, host.ID.String(), v1beta1.BoundCondition, v1beta1.UnbindingPendingUserActionReason)

	})

	It("SNO deploy clusterDeployment full install and validate MetaData", func() {
		By("Create cluster")
		labels := map[string]string{"foo": "bar", "Alice": "Bob"}
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		infraEnvSpec.AgentLabels = labels
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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		generateFullMeshConnectivity(ctx, ips[0], host)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
		By("Check that ACI Event URL is valid")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*cluster_id=%s", cluster.ID.String())))

		acicr := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
		_, err := testEventUrl(acicr.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Check ACI Logs URL is empty")
		// Should not show the URL since no logs were collected
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").Should(Equal(""))

		By("Ensure APIVIP exists in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.APIVIP
		}, "30s", "1s").Should(Equal(aciSNOSpec.APIVIP))

		By("Ensure IngressVIP exists in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.IngressVIP
		}, "30s", "1s").Should(Equal(aciSNOSpec.IngressVIP))

		By("verify platform type status")
		checkPlatformStatus(ctx, installkey, "", hiveext.NonePlatformType, swag.Bool(true))

		By("Verify Agent labels")
		labels[v1beta1.InfraEnvNameLabel] = infraNsName.Name
		agentHasAllLabels := func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			allFound := true
			for labelKey, labelValue := range labels {
				val, ok := agent.ObjectMeta.Labels[labelKey]
				if !ok || val != labelValue {
					allFound = false
					break
				}
			}
			return allFound
		}
		Eventually(agentHasAllLabels, "30s", "1s").Should(Equal(true))

		Eventually(func() error {
			a := getAgentCRD(ctx, kubeClient, key)
			delete(a.Labels, v1beta1.InfraEnvNameLabel)
			return kubeClient.Update(ctx, a)
		}, "30s", "2s").Should(Succeed())
		Eventually(agentHasAllLabels, "30s", "1s").Should(Equal(true))

		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Check that Agent Event URL is valid")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*host_id=%s", host.ID.String())))

		agent := getAgentCRD(ctx, kubeClient, key)
		_, err = testEventUrl(agent.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Wait for installing")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstallationInProgressReason)

		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		By("verify cluster progress on installation start")
		Eventually(func() int64 {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.Progress.TotalPercentage
		}, "30s", "10s").Should(Equal(int64(10)))

		By("verify cluster and host progress after hosts are installed")
		installProgress := models.HostStageDone
		installInfo := "Great Success"
		stages := []models.HostStage{
			models.HostStageStartingInstallation, models.HostStageInstalling,
			models.HostStageWaitingForBootkube, models.HostStageWritingImageToDisk,
			models.HostStageRebooting, models.HostStageJoined, models.HostStageDone,
		}
		updateHostProgressWithInfo(*host.ID, *infraEnv.ID, installProgress, installInfo)

		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.Progress.ProgressInfo == installInfo &&
				agent.Status.Progress.CurrentStage == installProgress &&
				agent.Status.Progress.InstallationPercentage == int64(100) &&
				reflect.DeepEqual(agent.Status.Progress.ProgressStages, stages)
		}, "30s", "10s").Should(BeTrue())

		Eventually(func() int64 {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.Progress.TotalPercentage
		}, "30s", "10s").Should(Equal(int64(80)))

		By("Check ACI Logs URL exists")
		kubeconfigFile, err := os.Open("test_kubeconfig")
		Expect(err).NotTo(HaveOccurred())
		_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: *cluster.ID,
			InfraEnvID: infraEnv.ID, HostID: host.ID, LogsType: string(models.LogsTypeHost), Upfile: kubeconfigFile})
		Expect(err).NotTo(HaveOccurred())
		kubeconfigFile.Close()

		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Check Agent Role and Bootstrap")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.Bootstrap && agent.Status.Role == models.HostRoleMaster
		}, "30s", "10s").Should(BeTrue())

		By("Check kubeconfig before install is finished")
		configSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
		configkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      configSecretRef.Name,
		}
		configSecret := getSecret(ctx, kubeClient, configkey)
		Expect(configSecret.Data["kubeconfig"]).NotTo(BeNil())

		By("Upload cluster logs")
		kubeconfigFile, err1 := os.Open("test_kubeconfig")
		Expect(err1).NotTo(HaveOccurred())
		_, err1 = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: *cluster.ID,
			InfraEnvID: infraEnv.ID, Upfile: kubeconfigFile, LogsType: string(models.LogsTypeController)})
		Expect(err1).NotTo(HaveOccurred())
		kubeconfigFile.Close()

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err = agentBMClient.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verify Cluster Metadata")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterFailedCondition, hiveext.ClusterNotFailedReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterStoppedCondition, hiveext.ClusterStoppedCompletedReason)

		passwordSecretRef := getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminPasswordSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		passwordkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      passwordSecretRef.Name,
		}
		passwordSecret := getSecret(ctx, kubeClient, passwordkey)
		Expect(passwordSecret.Data["password"]).NotTo(BeNil())
		Expect(passwordSecret.Data["username"]).NotTo(BeNil())
		configSecretRef = getAgentClusterInstallCRD(ctx, kubeClient, installkey).Spec.ClusterMetadata.AdminKubeconfigSecretRef
		Expect(passwordSecretRef).NotTo(BeNil())
		configSecret = getSecret(ctx, kubeClient, configkey)
		Expect(configSecret.Data["kubeconfig"]).NotTo(BeNil())

		By("Check that Event URL is still valid")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*cluster_id=%s", cluster.ID.String())))

		acicr = getAgentClusterInstallCRD(ctx, kubeClient, installkey)
		_, err = testEventUrl(acicr.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Check ACI Logs URL still exists")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Check ACI DebugInfo state and stateinfo")
		Eventually(func() bool {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.State == models.ClusterStatusAddingHosts &&
				aci.Status.DebugInfo.StateInfo == clusterInstallStateInfo
		}, "1m", "10s").Should(BeTrue())

		By("Check Agent DebugInfo state and stateinfo")
		Eventually(func() bool {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.State == models.HostStatusInstalled &&
				agent.Status.DebugInfo.StateInfo == doneStateInfo
		}, "1m", "10s").Should(BeTrue())
	})

	It("Move Agent to another infraenv", func() {
		By("Create first cluster and infraenv")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		host := registerNode(ctx, *infraEnv.ID, "hostname1", ips[0])
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		By("Check that Agent Event URL is valid")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*host_id=%s", host.ID.String())))
		firstAgentEventsURL := getAgentCRD(ctx, kubeClient, key).Status.DebugInfo.EventsURL

		agent := getAgentCRD(ctx, kubeClient, key)
		_, err := testEventUrl(agent.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Create New Infraenv")
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec2 := getDefaultClusterDeploymentSpec(secretRef)
		aciSNOSpec2 := getDefaultSNOAgentClusterInstallSpec(clusterDeploymentSpec2.ClusterName)
		infraEnvSpec2 := getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec2)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec2)
		deployInfraEnvCRD(ctx, kubeClient, "infraenv2", infraEnvSpec2)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec2, clusterDeploymentSpec2.ClusterInstallRef.Name)

		By("Register Agent to new Infraenv")
		infraEnv2Key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      "infraenv2",
		}
		infraEnv2 := getInfraEnvFromDBByKubeKey(ctx, db, infraEnv2Key, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv2.ID.String())
		h := &registerHostByUUID(*infraEnv2.ID, *host.ID).Host
		generateEssentialHostSteps(ctx, h, "hostname2", ips[1])
		generateEssentialPrepareForInstallationSteps(ctx, h)

		By("Check Agent is updated with new ClusterDeployment")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Spec.ClusterDeploymentName.Name
		}, "30s", "10s").Should(Equal(clusterDeploymentSpec2.ClusterName))

		By("Check Agent event URL has not changed")
		Eventually(func() string {
			agent := getAgentCRD(ctx, kubeClient, key)
			return agent.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(Equal(firstAgentEventsURL))

		By("Check host is removed from first backend cluster")
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(len(cluster.Hosts)).Should(Equal(0))

		By("Delete Original Clusterdeployment")
		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKey)
		Expect(kubeClient.Delete(ctx, clusterDeploymentCRD)).ShouldNot(HaveOccurred())

		By("Check Agent still exists")
		getAgentCRD(ctx, kubeClient, key)
	})

	It("SNO deploy clusterDeployment delete while install", func() {
		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		generateFullMeshConnectivity(ctx, ips[0], host)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
		By("Approve Agent")
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, key)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Wait for installing")

		Eventually(func() string {
			condition := controllers.FindStatusCondition(getAgentClusterInstallCRD(ctx, kubeClient, installkey).Status.Conditions, hiveext.ClusterCompletedCondition)
			if condition != nil {
				return condition.Message
			}
			return ""
		}, "2m", "2s").Should(HaveSuffix("Installation in progress"))

		By("Delete clusterDeployment")
		clusterDeploymentCRD := getClusterDeploymentCRD(ctx, kubeClient, clusterKey)
		Expect(kubeClient.Delete(ctx, clusterDeploymentCRD)).ShouldNot(HaveOccurred())

		By("Verify cluster record is deleted")
		Eventually(func() bool {
			_, err := common.GetClusterFromDBWhere(db, common.UseEagerLoading, common.SkipDeletedRecords, "kube_key_name = ? and kube_key_namespace = ?", clusterKey.Name, clusterKey.Namespace)
			return errors.Is(err, gorm.ErrRecordNotFound)
		}, "1m", "10s").Should(BeTrue())

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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsPassingReason)
		}
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsFailingReason)
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, host := range hosts {
			generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstallationInProgressReason)
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		for _, host := range hosts {
			checkAgentCondition(ctx, host.ID.String(), v1beta1.InstalledCondition, v1beta1.InstallationInProgressReason)
		}

		for _, host := range hosts {
			updateProgress(*host.ID, *infraEnv.ID, models.HostStageDone)
		}

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)

		By("Verify cluster transformed from day 1 to day 2")
		oldClusterID := cluster.ID
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(cluster.ID.String()).Should(Equal(oldClusterID.String()))
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))
		Expect(*cluster.Status).Should(Equal(models.ClusterStatusAddingHosts))

		By("Verify ClusterDeployment Agents were not deleted")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(3))

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
	It("None SNO deploy clusterDeployment full install", func() {
		By("Create cluster")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		hosts := make([]*models.Host, 0)
		ips := hostutil.GenerateIPv4Addresses(5, defaultCIDRv4)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := registerNode(ctx, *infraEnv.ID, hostname, ips[i])
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		for _, h := range hosts {
			generateDomainResolution(ctx, h, clusterDeploymentSpec.ClusterName, "hive.example.com")
		}
		By("Check ACI Logs URL is empty")
		// Should not show the URL since no logs yet to be collected
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").Should(Equal(""))
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstallationInProgressReason)
		Eventually(func() bool {
			c := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
			for _, h := range c.Hosts {
				if !funk.ContainsString([]string{models.HostStatusInstalling}, swag.StringValue(h.Status)) {
					return false
				}
			}
			return true
		}, "1m", "2s").Should(BeTrue())

		By("Upload hosts logs during installation")
		for _, host := range hosts {
			updateProgress(*host.ID, *infraEnv.ID, models.HostStageDone)

			kubeconfigFile, err := os.Open("test_kubeconfig")
			Expect(err).NotTo(HaveOccurred())
			_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: *cluster.ID,
				InfraEnvID: infraEnv.ID, HostID: host.ID, LogsType: string(models.LogsTypeHost), Upfile: kubeconfigFile})
			Expect(err).NotTo(HaveOccurred())
			kubeconfigFile.Close()
		}

		By("Check ACI Logs URL exists")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Upload cluster logs")
		kubeconfigFile, err1 := os.Open("test_kubeconfig")
		Expect(err1).NotTo(HaveOccurred())
		_, err1 = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: *cluster.ID,
			InfraEnvID: infraEnv.ID, Upfile: kubeconfigFile, LogsType: string(models.LogsTypeController)})
		Expect(err1).NotTo(HaveOccurred())
		kubeconfigFile.Close()

		By("Check ACI Logs URL still exists")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.LogsURL
		}, "30s", "10s").ShouldNot(Equal(""))

		By("Ensure APIVIP exists in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.APIVIP
		}, "30s", "1s").Should(Equal(aciSpec.APIVIP))

		By("Ensure APIVIPs exist in status")
		Eventually(func() int {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return len(aci.Status.APIVIPs)
		}, "30s", "1s").ShouldNot(Equal(0))

		By("Ensure correct APIVIPs exist in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.APIVIPs[0]
		}, "30s", "1s").Should(Equal(aciSpec.APIVIP))

		By("Ensure IngressVIP exists in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.IngressVIP
		}, "30s", "1s").Should(Equal(aciSpec.IngressVIP))

		By("Ensure IngressVIPs exists in status")
		Eventually(func() int {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return len(aci.Status.IngressVIPs)
		}, "30s", "1s").ShouldNot(Equal(0))

		By("Ensure correct IngressVIPs exist in status")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.IngressVIPs[0]
		}, "30s", "1s").Should(Equal(aciSpec.IngressVIP))

		By("Complete Installation")
		completeInstallation(agentBMClient, *cluster.ID)
		isSuccess := true
		_, err := agentBMClient.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
			ClusterID: *cluster.ID,
			CompletionParams: &models.CompletionParams{
				IsSuccess: &isSuccess,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		By("Verify cluster transformed from day 1 to day 2")
		oldClusterID := cluster.ID
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterInstalledReason)
		cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		Expect(cluster.ID.String()).Should(Equal(oldClusterID.String()))
		Expect(*cluster.Kind).Should(Equal(models.ClusterKindAddHostsCluster))
		Expect(*cluster.Status).Should(Equal(models.ClusterStatusAddingHosts))

		By("Check that ACI Event URL is valid")
		Eventually(func() string {
			aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
			return aci.Status.DebugInfo.EventsURL
		}, "30s", "10s").Should(MatchRegexp(fmt.Sprintf("/v2/events.*cluster_id=%s", cluster.ID.String())))

		aci := getAgentClusterInstallCRD(ctx, kubeClient, installkey)
		_, err = testEventUrl(aci.Status.DebugInfo.EventsURL)
		Expect(err).To(BeNil())

		By("Verify ClusterDeployment Agents were not deleted")

		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(3))

		By("Add Day 2 host")
		configureLocalAgentClient(infraEnv.ID.String())
		day2Host1 := registerNode(ctx, *infraEnv.ID, "firsthostnameday2", ips[3])
		generateApiVipPostStepReply(ctx, day2Host1, true)
		generateFullMeshConnectivity(ctx, ips[3], day2Host1)
		generateDomainResolution(ctx, day2Host1, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Add a second Day 2 host")
		day2Host2 := registerNode(ctx, *infraEnv.ID, "secondhostnameday2", ips[4])
		generateApiVipPostStepReply(ctx, day2Host2, true)
		generateFullMeshConnectivity(ctx, ips[4], day2Host2)
		generateDomainResolution(ctx, day2Host2, clusterDeploymentSpec.ClusterName, "hive.example.com")

		By("Approve Day 2 agents")
		k1 := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      day2Host1.ID.String(),
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, k1)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		k2 := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      day2Host2.ID.String(),
		}
		Eventually(func() error {
			agent := getAgentCRD(ctx, kubeClient, k2)
			agent.Spec.Approved = true
			return kubeClient.Update(ctx, agent)
		}, "30s", "10s").Should(BeNil())

		By("Verify Day 2 agents conditions")
		checkAgentCondition(ctx, day2Host1.ID.String(), v1beta1.InstalledCondition, v1beta1.InstallationInProgressReason)
		checkAgentCondition(ctx, day2Host1.ID.String(), v1beta1.RequirementsMetCondition, v1beta1.AgentAlreadyInstallingReason)
		checkAgentCondition(ctx, day2Host1.ID.String(), v1beta1.SpecSyncedCondition, v1beta1.SyncedOkReason)
		checkAgentCondition(ctx, day2Host1.ID.String(), v1beta1.ConnectedCondition, v1beta1.AgentConnectedReason)
		checkAgentCondition(ctx, day2Host1.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsPassingReason)
		checkAgentCondition(ctx, day2Host2.ID.String(), v1beta1.InstalledCondition, v1beta1.InstallationInProgressReason)
		checkAgentCondition(ctx, day2Host2.ID.String(), v1beta1.RequirementsMetCondition, v1beta1.AgentAlreadyInstallingReason)
		checkAgentCondition(ctx, day2Host2.ID.String(), v1beta1.SpecSyncedCondition, v1beta1.SyncedOkReason)
		checkAgentCondition(ctx, day2Host2.ID.String(), v1beta1.ConnectedCondition, v1beta1.AgentConnectedReason)
		checkAgentCondition(ctx, day2Host2.ID.String(), v1beta1.ValidatedCondition, v1beta1.ValidationsPassingReason)
	})

	It("deploy clusterDeployment with invalid machine cidr", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSNOSpec.Networking.MachineNetwork = []hiveext.MachineNetworkEntry{{CIDR: "1.2.3.5/24"}}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
	})

	It("deploy clusterDeployment without machine cidr", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSNOSpec.Networking.MachineNetwork = []hiveext.MachineNetworkEntry{}
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSNOSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
	})

	It("deploy clusterDeployment with invalid clusterImageSet", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		aciSpec.ImageSetRef.Name = "invalid"
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterBackendErrorReason)
	})

	It("deploy clusterDeployment with missing clusterImageSet", func() {
		// Remove ClusterImageSet that was created in the test setup
		imageSetKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      aciSpec.ImageSetRef.Name,
		}
		Expect(kubeClient.Delete(ctx, getClusterImageSetCRD(ctx, kubeClient, imageSetKey))).ShouldNot(HaveOccurred())
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterBackendErrorReason)

		// Create ClusterImageSet
		deployClusterImageSetCRD(ctx, kubeClient, aciSpec.ImageSetRef)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)
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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		generateFullMeshConnectivity(ctx, ips[0], host)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterBackendErrorReason)

		By("Deploy bad config map")
		data := map[string]string{"test.yaml": content, "test.dc": "test"}
		cm := deployOrUpdateConfigMap(ctx, kubeClient, ref.Name, data)
		defer func() {
			_ = kubeClient.Delete(ctx, cm)
		}()

		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)

		By("Fixing configmap and expecting installation to start")
		// adding sleep to be sure all reconciles will finish, will test that requeue worked as expected
		time.Sleep(30 * time.Second)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)

		data = map[string]string{"test.yaml": content, "test2.yaml": content}
		deployOrUpdateConfigMap(ctx, kubeClient, ref.Name, data)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
	})

	It("deploy agentClusterInstall with multiple manifest references (in invalid format and then fix it)", func() {
		By("Create cluster")
		configMapName1 := "cluster-install-config-1"
		configMapName2 := "cluster-install-config-2"
		refs := []hiveext.ManifestsConfigMapReference{
			{Name: configMapName1},
			{Name: configMapName2},
		}
		aciSNOSpec.ManifestsConfigMapRefs = refs
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
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		ips := hostutil.GenerateIPv4Addresses(1, defaultCIDRv4)
		generateFullMeshConnectivity(ctx, ips[0], host)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
		generateDomainResolution(ctx, host, clusterDeploymentSpec.ClusterName, "hive.example.com")
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterBackendErrorReason)

		By("Deploy bad config map")
		data1 := map[string]string{"test1.yaml": content, "test.dc.1": "test"}
		data2 := map[string]string{"test2.yaml": content, "test.dc.2": "test"}
		cm1 := deployOrUpdateConfigMap(ctx, kubeClient, refs[0].Name, data1)
		defer func() {
			_ = kubeClient.Delete(ctx, cm1)
		}()
		cm2 := deployOrUpdateConfigMap(ctx, kubeClient, refs[1].Name, data2)
		defer func() {
			_ = kubeClient.Delete(ctx, cm2)
		}()

		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)

		By("Fixing configmap and expecting installation to start")
		// adding sleep to be sure all reconciles will finish, will test that requeue worked as expected
		time.Sleep(30 * time.Second)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterReadyReason)

		deployOrUpdateConfigMap(ctx, kubeClient, refs[0].Name, map[string]string{"test1.yaml": content})
		deployOrUpdateConfigMap(ctx, kubeClient, refs[1].Name, map[string]string{"test2.yaml": content})
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
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
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterNotReadyReason)
		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		By("Validate no hosts currently belong to the cluster and clusterDeployment")
		Eventually(func() int {
			return len(getClusterDeploymentAgents(ctx, kubeClient, clusterKey).Items)
		}, "2m", "2s").Should(Equal(0))

		Eventually(func() int {
			return len(getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout).Hosts)
		}, "2m", "2s").Should(Equal(0))

		By("Register a Host and validate that an agent CR was created")
		configureLocalAgentClient(infraEnv.ID.String())
		registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
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

	It("Verify garbage collector inactive cluster deregistration isn't triggered for clusters created via ClusterDeployment", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}

		By("Update cluster's updated_at attribute to become eligible for deregistration due to inactivity")
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)
		db.Model(&cluster).UpdateColumn("updated_at", time.Now().AddDate(-1, 0, 0))

		By("Verify no event for deregistering the cluster was emitted")
		msg := "Cluster is deregistered"
		Consistently(func() []*common.Event {
			cluster = getClusterFromDB(ctx, kubeClient, db, clusterKey, 1)
			eventsHandler := events.New(db, nil, nil, logrus.New())
			events, err := eventsHandler.V2GetEvents(ctx, cluster.ID, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			return events
		}, "15s", "5s").ShouldNot(ContainElement(eventMatcher{event: &common.Event{Event: models.Event{Message: &msg, ClusterID: cluster.ID}}}))
	})

	It("Verify garbage collector deletes unregistered cluster for clusters created via ClusterDeployment", func() {
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
		clusterKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterName,
		}
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, clusterKey, waitForReconcileTimeout)

		By("Delete clusterdeployment to deregister the cluster")
		Expect(kubeClient.Delete(ctx, getAgentClusterInstallCRD(ctx, kubeClient, installkey))).ShouldNot(HaveOccurred())
		Expect(kubeClient.Delete(ctx, getClusterDeploymentCRD(ctx, kubeClient, clusterKey))).ShouldNot(HaveOccurred())

		By("Verify cluster record remains in the DB")
		Consistently(func() error {
			_, err := common.GetClusterFromDBWhere(db, common.UseEagerLoading, common.IncludeDeletedRecords, "kube_key_name = ? and kube_key_namespace = ?", clusterKey.Name, clusterKey.Namespace)
			return err
		}, "15s", "5s").ShouldNot(HaveOccurred())

		By("Update cluster's deleted_at attribute to become eligible for permanent removal")
		db.Unscoped().Where("id = ?", cluster.ID.String()).Model(&cluster).UpdateColumn("deleted_at", time.Now().AddDate(-1, 0, 0))

		By("Fetch cluster to make sure it was permanently removed by the garbage collector")
		Eventually(func() error {
			_, err := common.GetClusterFromDBWhere(db, common.UseEagerLoading, common.IncludeDeletedRecords, "kube_key_name = ? and kube_key_namespace = ?", clusterKey.Name, clusterKey.Namespace)
			return err
		}, "1m", "10s").Should(HaveOccurred())
	})

	It("deploy AgentClusterInstall with missing ClusterDeployment", func() {
		By("Create AgentClusterInstall")
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		By("Verify InputError")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterInputErrorReason)

		By("Create ClusterDeployment")
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)

		By("Verify SyncOK")
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)
		checkAgentClusterInstallConditionConsistency(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)
	})

	It("import installed cluster", func() {
		By("deploy installed cluster deployment")
		clusterDeploymentSpec.Installed = true
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
		By("deploy agent cluster install")
		deployAgentClusterInstallCRD(ctx, kubeClient, aciSpec, clusterDeploymentSpec.ClusterInstallRef.Name)

		By("check ACI conditions")
		installkey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      clusterDeploymentSpec.ClusterInstallRef.Name,
		}
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterSpecSyncedCondition, hiveext.ClusterSyncedOkReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterCompletedCondition, hiveext.ClusterStoppedCompletedReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterRequirementsMetCondition, hiveext.ClusterAlreadyInstallingReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterValidatedCondition, hiveext.ClusterValidationsPassingReason)
		checkAgentClusterInstallCondition(ctx, installkey, hiveext.ClusterFailedCondition, hiveext.ClusterNotFailedReason)
	})

})

var _ = Describe("bmac reconcile flow", func() {
	if !Options.EnableKubeAPI {
		return
	}

	ctx := context.Background()

	var (
		bmh         *metal3_v1alpha1.BareMetalHost
		bmhNsName   types.NamespacedName
		agentNsName types.NamespacedName
		infraNsName types.NamespacedName
	)

	BeforeEach(func() {
		secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
		clusterDeploymentSpec := getDefaultClusterDeploymentSpec(secretRef)
		deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)

		infraNsName = types.NamespacedName{
			Name:      "infraenv",
			Namespace: Options.Namespace,
		}
		infraEnvSpec := getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec)
		deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

		infraEnvKey := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      infraNsName.Name,
		}
		infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
		configureLocalAgentClient(infraEnv.ID.String())
		host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)
		agentNsName = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}

		bmhSpec := metal3_v1alpha1.BareMetalHostSpec{}
		deployBMHCRD(ctx, kubeClient, host.ID.String(), &bmhSpec)
		bmhNsName = types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      host.ID.String(),
		}
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

			Expect(bmh.Spec.AutomatedCleaningMode).To(Equal(metal3_v1alpha1.CleaningModeDisabled))
			Expect(bmh.ObjectMeta.Annotations).To(HaveKey(controllers.BMH_INSPECT_ANNOTATION))
			Expect(bmh.ObjectMeta.Annotations[controllers.BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
		})

		It("reconciles the agent", func() {
			bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
			bmh.Spec.BootMACAddress = getAgentMac(ctx, kubeClient, agentNsName)
			bmh.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, bmh)).ToNot(HaveOccurred())

			Eventually(func() bool {
				// expect bmh hardware annotation to be set
				bmh = getBmhCRD(ctx, kubeClient, bmhNsName)
				if bmh.ObjectMeta.Annotations == nil || bmh.ObjectMeta.Annotations[controllers.BMH_HARDWARE_DETAILS_ANNOTATION] == "" {
					return false
				}
				// expect agent bmh reference to be set
				agent := getAgentCRD(ctx, kubeClient, agentNsName)
				if agent.Labels == nil || agent.Labels[controllers.AGENT_BMH_LABEL] != bmh.Name {
					return false
				}
				return true
			}, "60s", "10s").Should(Equal(true))
		})
	})
})

var _ = Describe("PreprovisioningImage reconcile flow", func() {
	if !Options.EnableKubeAPI {
		return
	}
	ctx := context.Background()

	It("will correctly set the image url after an invalid infraenv is corrected", func() {
		infraNsName := types.NamespacedName{
			Name:      "infraenv",
			Namespace: Options.Namespace,
		}
		infraEnv := &v1beta1.InfraEnv{
			TypeMeta: metav1.TypeMeta{
				Kind:       "InfraEnv",
				APIVersion: getAPIVersion(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   Options.Namespace,
				Name:        infraNsName.Name,
				Annotations: map[string]string{controllers.EnableIronicAgentAnnotation: "true"},
			},
			Spec: v1beta1.InfraEnvSpec{
				PullSecretRef:    deployLocalObjectSecretIfNeeded(ctx, kubeClient),
				SSHAuthorizedKey: "invalid",
			},
		}
		Expect(kubeClient.Create(ctx, infraEnv)).To(Succeed())

		ppiNsName := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      "test-image",
		}
		ppi := &metal3_v1alpha1.PreprovisioningImage{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PreprovisioningImage",
				APIVersion: "metal3.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Options.Namespace,
				Name:      ppiNsName.Name,
				Labels:    map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name},
			},
			Spec: metal3_v1alpha1.PreprovisioningImageSpec{
				AcceptFormats: []metal3_v1alpha1.ImageFormat{
					metal3_v1alpha1.ImageFormatISO,
				},
			},
		}
		Expect(kubeClient.Create(ctx, ppi)).To(Succeed())

		// check for not created condition
		Eventually(func() bool {
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			if readyCondition != nil &&
				readyCondition.Status == metav1.ConditionFalse &&
				readyCondition.Message == "Waiting for InfraEnv image to be created" {
				return true
			}
			return false
		}, "30s", "5s").Should(Equal(true))

		// correct public key
		Eventually(func() error {
			infraEnv := getInfraEnvCRD(ctx, kubeClient, infraNsName)
			infraEnv.Spec.SSHAuthorizedKey = sshPublicKey
			return kubeClient.Update(ctx, infraEnv)
		}, "30s", "5s").Should(Succeed())

		// ensure image gets set
		Eventually(func() bool {
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			if readyCondition == nil {
				return false
			}
			return readyCondition.Status == metav1.ConditionTrue
		}, "120s", "10s").Should(Equal(true))
		ppi = getPPICRD(ctx, kubeClient, ppiNsName)
		infraEnvURL := getInfraEnvFromDBByKubeKey(ctx, db, infraNsName, waitForReconcileTimeout).DownloadURL
		Expect(ppi.Status.ImageUrl).To(Equal(infraEnvURL))
	})

	Context("PPI with infraEnv label", func() {

		var (
			ppi         *metal3_v1alpha1.PreprovisioningImage
			ppiNsName   types.NamespacedName
			infraNsName types.NamespacedName
			infraEnvKey types.NamespacedName
		)

		BeforeEach(func() {
			secretRef := deployLocalObjectSecretIfNeeded(ctx, kubeClient)
			clusterDeploymentSpec := getDefaultClusterDeploymentSpec(secretRef)
			deployClusterDeploymentCRD(ctx, kubeClient, clusterDeploymentSpec)
			snoSpec := getDefaultSNOAgentClusterInstallSpec(clusterDeploymentSpec.ClusterName)
			deployAgentClusterInstallCRD(ctx, kubeClient, snoSpec, clusterDeploymentSpec.ClusterInstallRef.Name)
			deployClusterImageSetCRD(ctx, kubeClient, snoSpec.ImageSetRef)

			infraNsName = types.NamespacedName{
				Name:      "infraenv",
				Namespace: Options.Namespace,
			}
			infraEnvSpec := getDefaultInfraEnvSpec(secretRef, clusterDeploymentSpec)
			deployInfraEnvCRD(ctx, kubeClient, infraNsName.Name, infraEnvSpec)

			infraEnvKey = types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      infraNsName.Name,
			}
			infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
			configureLocalAgentClient(infraEnv.ID.String())
			host := registerNode(ctx, *infraEnv.ID, "hostname1", defaultCIDRv4)

			ppiSpec := metal3_v1alpha1.PreprovisioningImageSpec{AcceptFormats: []metal3_v1alpha1.ImageFormat{metal3_v1alpha1.ImageFormatISO}}
			deployPPICRD(ctx, kubeClient, host.ID.String(), &ppiSpec)
			ppiNsName = types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
		})

		It("should trigger an infraenv update", func() {
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			ppi.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, ppi)).ToNot(HaveOccurred())

			Eventually(func() bool {
				infraEnv := getInfraEnvCRD(ctx, kubeClient, infraNsName)
				value, ok := infraEnv.GetAnnotations()[controllers.EnableIronicAgentAnnotation]
				if !ok {
					return false
				}
				return value == "true"
			}, "30s", "10s").Should(Equal(true))
			infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
			Expect(infraEnv.InternalIgnitionConfigOverride).Should(ContainSubstring("ironic-agent.service"))
		})
		It("should get ImageUrl from the infraEnv", func() {
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			ppi.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, ppi)).ToNot(HaveOccurred())

			Eventually(func() bool {
				ppi = getPPICRD(ctx, kubeClient, ppiNsName)
				readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
				if readyCondition == nil {
					return false
				}
				return readyCondition.Status == metav1.ConditionTrue
			}, "120s", "10s").Should(Equal(true))
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			infraEnv := getInfraEnvFromDBByKubeKey(ctx, db, infraEnvKey, waitForReconcileTimeout)
			Expect(ppi.Status.ImageUrl).To(Equal(infraEnv.DownloadURL))
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		})
		It("and unsupported image format should have error condition set to true", func() {
			ppi = getPPICRD(ctx, kubeClient, ppiNsName)
			ppi.Spec.AcceptFormats = []metal3_v1alpha1.ImageFormat{metal3_v1alpha1.ImageFormatInitRD}
			ppi.SetLabels(map[string]string{controllers.BMH_INFRA_ENV_LABEL: infraNsName.Name})
			Expect(kubeClient.Update(ctx, ppi)).ToNot(HaveOccurred())

			// Wait for error condition status to be true
			Eventually(func() bool {
				ppi = getPPICRD(ctx, kubeClient, ppiNsName)
				log.Info(ppi)
				readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
				if readyCondition == nil {
					return false
				}
				return readyCondition.Status == metav1.ConditionTrue
			}, "30s", "10s").Should(Equal(true))
		})
	})
})

// custom matcher for events based on partial attributes (cluster ID and message)
type eventMatcher struct {
	event *common.Event
}

func (e eventMatcher) Match(input interface{}) (bool, error) {
	event, ok := input.(*common.Event)
	if !ok {
		return false, nil
	}
	if e.event.ClusterID != event.Event.ClusterID {
		return false, nil
	}
	if !strings.Contains(*event.Message, *e.event.Message) {
		return false, nil
	}
	return true, nil
}

func (e eventMatcher) FailureMessage(actual interface{}) string {
	return "event does not match"
}

func (e eventMatcher) NegatedFailureMessage(actual interface{}) string {
	return "event matches"
}

func testEventUrl(url string) (models.EventList, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	list := models.EventList{}
	err = json.Unmarshal(body, &list)
	if err != nil {
		return nil, err
	}
	Expect(list).NotTo(BeEmpty())
	return list, nil
}
