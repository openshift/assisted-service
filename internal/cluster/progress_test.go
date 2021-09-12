package cluster

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Progress bar test", func() {

	var (
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		ctrl            *gomock.Controller
		clusterApi      *Manager
		mockEvents      *events.MockHandler
		mockHostAPI     *host.MockAPI
		mockMetric      *metrics.MockAPI
		mockOperatorApi *operators.MockAPI
		mockDnsApi      *dns.MockDNSApi
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockOperatorApi = operators.NewMockAPI(ctrl)
		mockDnsApi = dns.NewMockDNSApi(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, mockOperatorApi, nil, nil, mockDnsApi)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	expectProgressToBe := func(c *common.Cluster, preparingForInstallationStagePercentage, installingStagePercentage, finalizingStagePercentage int) {

		Expect(c.Progress).NotTo(BeNil())
		Expect(c.Progress.PreparingForInstallationStagePercentage).To(Equal(int64(preparingForInstallationStagePercentage)))
		Expect(c.Progress.InstallingStagePercentage).To(Equal(int64(installingStagePercentage)))
		Expect(c.Progress.FinalizingStagePercentage).To(Equal(int64(finalizingStagePercentage)))
		totalPercentage := common.ProgressWeightPreparingForInstallationStage*float64(c.Progress.PreparingForInstallationStagePercentage) +
			common.ProgressWeightInstallingStage*float64(c.Progress.InstallingStagePercentage) +
			common.ProgressWeightFinalizingStage*float64(c.Progress.FinalizingStagePercentage)
		Expect(c.Progress.TotalPercentage).To(Equal(int64(totalPercentage)))
	}

	It("UpdateInstallProgress test", func() {

		var clusterId strfmt.UUID

		By("Create cluster", func() {

			clusterId = strfmt.UUID(uuid.New().String())
			hid1 := strfmt.UUID(uuid.New().String())
			hid2 := strfmt.UUID(uuid.New().String())
			hid3 := strfmt.UUID(uuid.New().String())
			hid4 := strfmt.UUID(uuid.New().String())
			hid5 := strfmt.UUID(uuid.New().String())
			c := common.Cluster{
				Cluster: models.Cluster{
					ID:   &clusterId,
					Kind: swag.String(models.ClusterKindCluster),
					Hosts: []*models.Host{
						{
							ID:         &hid1,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Role:       models.HostRoleBootstrap,
							Bootstrap:  true,
							Status:     swag.String(models.HostStatusInstalling),
						},
						{
							ID:         &hid2,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Role:       models.HostRoleMaster,
							Status:     swag.String(models.HostStatusInstalling),
						},
						{
							ID:         &hid3,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Role:       models.HostRoleMaster,
							Status:     swag.String(models.HostStatusInstalling),
						},
						{
							ID:         &hid4,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Role:       models.HostRoleWorker,
							Status:     swag.String(models.HostStatusInstalling),
						},
						{
							ID:         &hid5,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Role:       models.HostRoleWorker,
							Status:     swag.String(models.HostStatusInstalling),
						},
					},
				},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		})

		By("test progress", func() {

			bootstrapIndexReturned := 2
			mastersIndexReturned := 3
			workersIndexReturned := 1
			mockHostAPI.EXPECT().GetStagesByRole(models.HostRoleBootstrap, true).Return(host.BootstrapStages[:]).Times(1)
			mockHostAPI.EXPECT().GetStagesByRole(models.HostRoleMaster, false).Return(host.MasterStages[:]).Times(2)
			mockHostAPI.EXPECT().GetStagesByRole(models.HostRoleWorker, false).Return(host.WorkerStages[:]).Times(2)
			mockHostAPI.EXPECT().IndexOfStage(gomock.Any(), host.BootstrapStages[:]).Return(bootstrapIndexReturned).Times(1)
			mockHostAPI.EXPECT().IndexOfStage(gomock.Any(), host.MasterStages[:]).Return(mastersIndexReturned).Times(2)
			mockHostAPI.EXPECT().IndexOfStage(gomock.Any(), host.WorkerStages[:]).Return(workersIndexReturned).Times(2)

			err := clusterApi.UpdateInstallProgress(ctx, clusterId)
			Expect(err).NotTo(HaveOccurred())

			c := getClusterFromDB(clusterId, db)
			expectedInstallingStagePercentage := float64((bootstrapIndexReturned+1)+2*(mastersIndexReturned+1)+2*(workersIndexReturned+1)) * 100 /
				float64(len(host.BootstrapStages[:])+2*len(host.MasterStages[:])+2*len(host.WorkerStages[:]))
			expectProgressToBe(&c, 0, int(expectedInstallingStagePercentage), 0)
		})
	})

	It("UpdateFinalizingProgress test", func() {

		var clusterId strfmt.UUID

		By("Create cluster", func() {

			clusterId = strfmt.UUID(uuid.New().String())
			c := common.Cluster{
				Cluster: models.Cluster{
					ID: &clusterId,
					MonitoredOperators: []*models.MonitoredOperator{
						{
							Name:         operators.OperatorConsole.Name,
							OperatorType: models.OperatorTypeBuiltin,
							Status:       models.OperatorStatusAvailable,
						},
						{
							Name:         operators.OperatorCVO.Name,
							OperatorType: models.OperatorTypeBuiltin,
							Status:       models.OperatorStatusProgressing,
						},
						{
							Name:         "cnv",
							OperatorType: models.OperatorTypeOlm,
							Status:       models.OperatorStatusAvailable,
						},
						{
							Name:         "ocs",
							OperatorType: models.OperatorTypeOlm,
							Status:       models.OperatorStatusFailed,
						},
					},
				},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		})

		By("test progress", func() {

			err := clusterApi.UpdateFinalizingProgress(ctx, db, clusterId)
			Expect(err).NotTo(HaveOccurred())

			c := getClusterFromDB(clusterId, db)
			// built-in operator must succeed and OLM operators may fail (degraded cluster)
			expectProgressToBe(&c, 0, 0, 75)
		})
	})

	It("preparing-for-installation --> installing transition test", func() {

		var clusterId strfmt.UUID
		var c common.Cluster

		By("Create cluster", func() {

			clusterId = strfmt.UUID(uuid.New().String())
			hid1 := strfmt.UUID(uuid.New().String())
			hid2 := strfmt.UUID(uuid.New().String())
			hid3 := strfmt.UUID(uuid.New().String())
			c = common.Cluster{
				Cluster: models.Cluster{
					ID:              &clusterId,
					Status:          swag.String(models.ClusterStatusPreparingForInstallation),
					StatusUpdatedAt: strfmt.DateTime(time.Now()),
					Hosts: []*models.Host{
						{
							ID:         &hid1,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Status:     swag.String(models.HostStatusPreparingSuccessful),
						},
						{
							ID:         &hid2,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Status:     swag.String(models.HostStatusPreparingSuccessful),
						},
						{
							ID:         &hid3,
							ClusterID:  &clusterId,
							InfraEnvID: clusterId,
							Status:     swag.String(models.HostStatusPreparingSuccessful),
						},
					},
				},
				InstallationPreparationCompletionStatus: common.InstallationPreparationSucceeded,
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		})

		By("test progress", func() {

			mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(3)
			mockOperatorApi.EXPECT().ValidateCluster(ctx, gomock.Any()).Return([]api.ValidationResult{}, nil)
			mockDnsApi.EXPECT().CreateDNSRecordSets(ctx, gomock.Any()).Return(nil)
			mockMetric.EXPECT().InstallationStarted(gomock.Any(), clusterId, gomock.Any(), gomock.Any())
			mockMetric.EXPECT().ClusterHostInstallationCount(gomock.Any(), 3, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
				eventstest.WithClusterIdMatcher(clusterId.String())))

			cAfterRefresh, err := clusterApi.RefreshStatus(ctx, &c, db)
			Expect(err).NotTo(HaveOccurred())

			expectProgressToBe(cAfterRefresh, 100, 0, 0)
		})
	})
})
