package controllers

import (
	"context"

	strfmt "github.com/go-openapi/strfmt"
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
		infraEnv                *common.InfraEnv
		cluster                 *common.Cluster
		clusterName             = "test-cluster"
		clusterId               strfmt.UUID
		infraEnvId              strfmt.UUID
		agentClusterInstallName = "test-cluster-aci"
		infraEnvNamespace       = "infra-env-test-namespace"
		infraEnvName            = "infraEnvName"
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
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		infraEnv = &common.InfraEnv{
			KubeKeyNamespace: infraEnvNamespace,
			InfraEnv: models.InfraEnv{
				Name: &infraEnvName,
				ID:   &infraEnvId,
			},
		}
		cluster = &common.Cluster{
			KubeKeyName:      clusterName,
			KubeKeyNamespace: testNamespace,
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("create agent with cluster", func() {

		It("create agent success", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, cluster)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).ShouldNot(HaveOccurred())
		})

		It("create agent with labels", func() {
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
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, cluster)
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

		It("Empty infraenv name space- no cluster", func() {
			hostId := uuid.New().String()
			infraEnv.KubeKeyNamespace = ""
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, nil)
			namespacedName := types.NamespacedName{
				Namespace: "",
				Name:      hostId,
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Get(ctx, namespacedName, &v1beta1.Agent{})).Should(HaveOccurred())
		})

		It("Empty infraenv and cluster name space", func() {
			hostId := uuid.New().String()
			infraEnv.KubeKeyNamespace = ""
			cluster.KubeKeyNamespace = ""
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, nil)
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
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			hostId := uuid.New().String()
			agent := newAgent(hostId, infraEnvNamespace, v1beta1.AgentSpec{})
			Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())

			id := strfmt.UUID(hostId)
			h := common.Host{
				Host: models.Host{
					ID:        &id,
					ClusterID: &clusterId,
				},
			}
			mockHostApi.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&h, nil).Times(1)

			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Already existing agent different infraenv same namespace", func() {
			clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      clusterDeployment.Name,
					Namespace: clusterDeployment.Namespace,
				},
			})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			clusterId := strfmt.UUID(uuid.New().String())
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, cluster)
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
			infraEnvImage2Name := "infraEnvImage2"
			h := common.Host{
				Host: models.Host{
					ID:         &id,
					ClusterID:  &clusterId,
					InfraEnvID: infraEnvId,
				},
			}
			infraEnvId2 := strfmt.UUID(hostId)
			infraEnv2 := &common.InfraEnv{
				KubeKeyNamespace: infraEnvNamespace,
				InfraEnv: models.InfraEnv{
					ID:   &infraEnvId2,
					Name: &infraEnvImage2Name,
				},
			}
			mockHostApi.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&h, nil).Times(1)
			mockHostApi.EXPECT().UnRegisterHost(ctx, gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			err = crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv2, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("create agent without cluster", func() {

		It("create agent success no cluster", func() {
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			mockHostApi.EXPECT().UpdateKubeKeyNS(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, nil)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: infraEnvNamespace,
				Name:      hostId,
			}
			agent := &v1beta1.Agent{}
			Expect(c.Get(ctx, namespacedName, agent)).ShouldNot(HaveOccurred())
			Expect(agent.Spec.ClusterDeploymentName).To(BeNil())
		})

		It("Already existing agent - no cluster, same infraenv", func() {
			infraEnvImage := newInfraEnvImage(infraEnvName, infraEnvNamespace, v1beta1.InfraEnvSpec{})
			Expect(c.Create(ctx, infraEnvImage)).ShouldNot(HaveOccurred())

			hostId := uuid.New().String()
			agent := newAgent(hostId, infraEnvNamespace, v1beta1.AgentSpec{})
			Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())

			id := strfmt.UUID(hostId)
			h := common.Host{
				Host: models.Host{
					ID:         &id,
					InfraEnvID: infraEnvId,
				},
			}
			mockHostApi.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&h, nil).Times(1)

			err := crdUtils.CreateAgentCR(ctx, log, hostId, infraEnv, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
