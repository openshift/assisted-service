package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	agentv1 "github.com/openshift/hive/pkg/apis/hive/v1/agent"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
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
	data := map[string]string{"pullSecret": secret}
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

func getAgentCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1alpha1.Agent {
	agent := &v1alpha1.Agent{}
	err := client.Get(ctx, key, agent)
	Expect(err).To(BeNil())
	return agent
}

func waitForClusterDeploymentCRDState(
	ctx context.Context, client k8sclient.Client, key types.NamespacedName, state string, timeout int) {

	clusterDeployment := &hivev1.ClusterDeployment{}
	successInARaw := 0
	start := time.Now()
	for time.Duration(timeout)*time.Second > time.Since(start) {
		clusterDeployment = getClusterDeploymentCRD(ctx, client, key)
		if clusterDeployment.Status.Conditions[0].Message == state {
			successInARaw++
		} else {
			successInARaw = 0
		}
		if successInARaw == minSuccessesInRow {
			return
		}
		time.Sleep(time.Second)
	}
	Expect(clusterDeployment.Status.Conditions[0].Message).Should(Equal(state))
	successInARaw++
	Expect(successInARaw).Should(Equal(minSuccessesInRow))
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
				APIVIP:            "1.2.3.8",
				IngressVIP:        "1.2.3.9",
				VIPDHCPAllocation: agentv1.Disabled,
			},
		},
		PullSecretRef: secretRef,
	}
}

func cleanUP(ctx context.Context, client k8sclient.Client) {
	Expect(client.DeleteAllOf(ctx, &hivev1.ClusterDeployment{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1alpha1.Image{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1alpha1.Agent{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
}

func generateFAPostStepReply(ctx context.Context, h *models.Host, freeAddresses models.FreeNetworksAddresses) {
	fa, err := json.Marshal(&freeAddresses)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
		ClusterID: h.ClusterID,
		HostID:    *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(fa),
			StepID:   string(models.StepTypeFreeNetworkAddresses),
			StepType: models.StepTypeFreeNetworkAddresses,
		},
	})
	Expect(err).To(BeNil())
}

func setupNewHost(ctx context.Context, hostname string, clusterID strfmt.UUID) *models.Host {
	host := registerNode(ctx, clusterID, hostname)
	generateFAPostStepReply(ctx, host, validFreeAddresses)
	return host
}

var _ = Describe("[kube-api]cluster installation", func() {
	if !Options.EnableKubeAPI {
		return
	}

	ctx := context.Background()

	waitForClusterReconcileTimeout := 30

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
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForClusterReconcileTimeout)
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
		waitForClusterDeploymentCRDState(ctx, kubeClient, key, models.ClusterStatusPreparingForInstallation, waitForClusterReconcileTimeout)
		for _, host := range hosts {
			key = types.NamespacedName{
				Namespace: Options.Namespace,
				Name:      host.ID.String(),
			}
			getAgentCRD(ctx, kubeClient, key)
		}
	})
})
