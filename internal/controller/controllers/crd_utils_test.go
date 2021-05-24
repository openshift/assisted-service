package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("create agent CR", func() {
	var (
		c                       client.Client
		ctx                     = context.Background()
		mockCtrl                *gomock.Controller
		clusterName             = "test-cluster"
		agentClusterInstallName = "test-cluster-aci"
		clusterNamespace        = "test-namespace"
		infraEnvNamespace       = "infra-env-test-namespace"
		pullSecretName          = "pull-secret"
		crdUtils                *CRDUtils
		defaultClusterSpec      hivev1.ClusterDeploymentSpec
		log                     = common.GetTestLog().WithField("pkg", "controllers")
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		crdUtils = NewCRDUtils(c)
		mockCtrl = gomock.NewController(GinkgoT())
		defaultClusterSpec = getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("create agent", func() {

		It("create agent success", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
			infraEnvImage := newInfraEnvImage("infraEnvImage", infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).ShouldNot(HaveOccurred())
		})

		It("Empty name space", func() {
			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, "", clusterName)
			namespacedName := types.NamespacedName{
				Namespace: "",
				Name:      hostId,
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).Should(HaveOccurred())
		})

		It("Already existing agent", func() {
			id := uuid.New().String()
			agent := newAgent(id, clusterNamespace, v1beta1.AgentSpec{})
			Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())
			err := crdUtils.CreateAgentCR(ctx, log, id, clusterNamespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("missing InfraEnv in ClusterDeployment", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("No InfraEnv resource for ClusterDeployment"))
		})
	})

})
