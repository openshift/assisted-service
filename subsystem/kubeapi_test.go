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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func deployDefaultSecretIfNeeded(ctx context.Context, client k8sclient.Client) *corev1.SecretReference {
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
	return &corev1.SecretReference{
		Namespace: Options.Namespace,
		Name:      "pull-secret",
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

func deployClusterCRD(ctx context.Context, client k8sclient.Client, spec *v1alpha1.ClusterSpec) {
	err := client.Create(ctx, &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Options.Namespace,
			Name:      spec.Name,
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
		err = db.Take(&cluster, "kube_key_name = ? and kube_key_namespace = ?", key.Name, key.Namespace).Error
		if err == nil {
			return cluster
		}
		if !gorm.IsRecordNotFoundError(err) {
			Expect(err).To(BeNil())
		}
		clusterCRD := getClusterCRD(ctx, client, key)
		Expect(clusterCRD.Status.Error).Should(Equal(""))
		time.Sleep(time.Second)
	}
	Expect(err).To(BeNil())
	return cluster
}

func getClusterCRD(ctx context.Context, client k8sclient.Client, key types.NamespacedName) *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	err := client.Get(ctx, key, cluster)
	Expect(err).To(BeNil())
	return cluster
}

func waitForClusterCRDState(
	ctx context.Context, client k8sclient.Client, key types.NamespacedName, state string, timeout int) {

	cluster := &v1alpha1.Cluster{}
	successInARaw := 0
	start := time.Now()
	for time.Duration(timeout)*time.Second > time.Since(start) {
		cluster = getClusterCRD(ctx, client, key)
		if cluster.Status.State == state {
			successInARaw++
		} else {
			successInARaw = 0
		}
		if successInARaw == minSuccessesInRow {
			return
		}
		time.Sleep(time.Second)
	}
	Expect(cluster.Status.State).Should(Equal(state))
	successInARaw++
	Expect(successInARaw).Should(Equal(minSuccessesInRow))
}

func getDefaultClusterSpec(secretRef *corev1.SecretReference) *v1alpha1.ClusterSpec {
	return &v1alpha1.ClusterSpec{
		Name:                     "test-cluster",
		OpenshiftVersion:         "4.6",
		BaseDNSDomain:            "test.domain",
		ClusterNetworkCidr:       "10.128.0.0/14",
		ClusterNetworkHostPrefix: 23,
		ServiceNetworkCidr:       "172.30.0.0/16",
		IngressVip:               "1.2.3.9",
		APIVip:                   "1.2.3.8",
		SSHPublicKey:             sshPublicKey,
		VIPDhcpAllocation:        false,
		PullSecretRef:            secretRef,
		ProvisionRequirements: v1alpha1.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       0,
			AgentSelector:      metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}},
		},
	}
}

func cleanUP(ctx context.Context, client k8sclient.Client) {
	Expect(client.DeleteAllOf(ctx, &v1alpha1.Cluster{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
	Expect(client.DeleteAllOf(ctx, &v1alpha1.Image{}, k8sclient.InNamespace(Options.Namespace))).To(BeNil())
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

	waitForClusterReconcileTimeout := 10

	AfterEach(func() {
		cleanUP(ctx, kubeClient)
		clearDB()
	})

	It("deploy cluster with hosts and wait for preparing-for-installation", func() {
		secretRef := deployDefaultSecretIfNeeded(ctx, kubeClient)
		spec := getDefaultClusterSpec(secretRef)
		deployClusterCRD(ctx, kubeClient, spec)
		key := types.NamespacedName{
			Namespace: Options.Namespace,
			Name:      spec.Name,
		}
		cluster := getClusterFromDB(ctx, kubeClient, db, key, waitForClusterReconcileTimeout)
		hosts := make([]*models.Host, 0)
		for i := 0; i < 3; i++ {
			hostname := fmt.Sprintf("h%d", i)
			host := setupNewHost(ctx, hostname, *cluster.ID)
			hosts = append(hosts, host)
		}
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)

		waitForClusterCRDState(ctx, kubeClient, key, models.ClusterStatusPreparingForInstallation, waitForClusterReconcileTimeout)
	})
})
