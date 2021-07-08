package controllers

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
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
		mockHostApi             *host.MockAPI
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
		mockCtrl = gomock.NewController(GinkgoT())
		mockHostApi = host.NewMockAPI(mockCtrl)
		crdUtils = NewCRDUtils(c, mockHostApi)
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

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, nil)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).ShouldNot(HaveOccurred())
		})

		It("create agent with labels", func() {
			infraEnvName := "infraEnvImage"
			labels := map[string]string{"foo": "bar", "Alice": "Bob"}
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
				AgentLabels: labels,
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, nil)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			agent := &v1beta1.Agent{}
			Expect(c.Get(ctx, namespacedName, agent)).ShouldNot(HaveOccurred())
			labels[v1beta1.InfraEnvNameLabel] = infraEnvName
			Expect(agent.ObjectMeta.Labels).Should(Equal(labels))
		})

		It("Empty name space", func() {
			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, "", clusterName, nil)
			namespacedName := types.NamespacedName{
				Namespace: "",
				Name:      hostId,
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).Should(HaveOccurred())
		})

		It("Already existing agent", func() {
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
			agent := newAgent(hostId, infraEnvNamespace, v1beta1.AgentSpec{})
			Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())
			clusterId := strfmt.UUID(uuid.New().String())
			id := strfmt.UUID(hostId)
			h := common.Host{
				Host: models.Host{
					ID:        &id,
					ClusterID: clusterId,
				},
			}
			mockHostApi.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&h, nil).Times(1)

			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, &clusterId)
			Expect(err).NotTo(HaveOccurred())
		})

		It("missing InfraEnv in ClusterDeployment", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("No InfraEnv resource for ClusterDeployment"))
		})

		It("Already existing agent different infraenv", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
			infraEnvImage := newInfraEnvImage("infraEnvImage", infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			clusterId := strfmt.UUID(uuid.New().String())
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, &clusterId)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).ShouldNot(HaveOccurred())

			clusterDeployment2 := newClusterDeployment("test-cluster2", testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment2)).ShouldNot(HaveOccurred())
			infraEnvImage2 := newInfraEnvImage("infraEnvImage2", infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment2.Name,
					Namespace: clusterDeployment2.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage2)).ShouldNot(HaveOccurred())

			id := strfmt.UUID(hostId)
			h := common.Host{
				Host: models.Host{
					ID:        &id,
					ClusterID: clusterId,
				},
			}
			mockHostApi.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&h, nil).Times(1)
			mockHostApi.EXPECT().UnRegisterHost(ctx, id.String(), clusterId.String()).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			clusterId2 := strfmt.UUID(uuid.New().String())
			err = crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName, &clusterId2)
			Expect(err).NotTo(HaveOccurred())
		})
	})

})
