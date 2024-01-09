package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/uploader"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
)

func getDefaultConfig() Config {
	var cfg Config
	Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
	return cfg
}

var _ = Describe("stateMachine", func() {
	var (
		ctx                = context.Background()
		db                 *gorm.DB
		state              API
		cluster            *common.Cluster
		refreshedCluster   *common.Cluster
		stateErr           error
		dbName             string
		mockEventsUploader *uploader.MockClient
		mockOperators      *operators.MockAPI
		mockS3Client       *s3wrapper.MockAPI
		ctrl               *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockOperators = operators.NewMockAPI(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), nil, mockEventsUploader, nil, nil, nil, dummy, mockOperators, nil, mockS3Client, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("unknown_cluster_state", func() {
		BeforeEach(func() {
			id := strfmt.UUID(uuid.New().String())
			cluster = &common.Cluster{Cluster: models.Cluster{
				ID:         &id,
				StatusInfo: swag.String("not a known state"),
			}}

			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
				{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
				{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
				{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
				{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
				{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
			}, nil)
		})

		It("update_cluster", func() {
			refreshedCluster, stateErr = state.RefreshStatus(ctx, cluster, db)
		})

		AfterEach(func() {
			Expect(refreshedCluster).To(BeNil())
			Expect(stateErr).Should(HaveOccurred())
		})
	})
})

/*
All supported case options:
installing -> installing
installing -> installed
installing -> error

known -> insufficient
insufficient -> known
*/

var _ = Describe("TestClusterMonitoring", func() {
	var (
		db                 *gorm.DB
		c                  common.Cluster
		id                 strfmt.UUID
		err                error
		clusterApi         *Manager
		shouldHaveUpdated  bool
		expectedState      string
		ctrl               *gomock.Controller
		mockHostAPI        *host.MockAPI
		mockMetric         *metrics.MockAPI
		dbName             string
		mockEvents         *eventsapi.MockHandler
		mockEventsUploader *uploader.MockClient
		mockS3Client       *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockEventsUploader = uploader.NewMockClient(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, mockEventsUploader, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, mockS3Client, nil, nil, nil)
		expectedState = ""
		shouldHaveUpdated = false

		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), &id, mockEvents).AnyTimes()
		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("single cluster monitoring", func() {
		createCluster := func(id *strfmt.UUID, status, statusInfo string) common.Cluster {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 id,
					Status:             swag.String(status),
					StatusInfo:         swag.String(statusInfo),
					MachineNetworks:    common.TestIPv4Networking.MachineNetworks,
					BaseDNSDomain:      "test.com",
					PullSecretSet:      true,
					MonitoredOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator},
					StatusUpdatedAt:    strfmt.DateTime(time.Now()),
				},
				TriggerMonitorTimestamp: time.Now(),
			}
			Expect(common.LoadTableFromDB(db, common.MonitoredOperatorsTable).Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(err).ShouldNot(HaveOccurred())

			return cluster
		}

		Context("from installing state", func() {
			BeforeEach(func() {
				c = createCluster(&id, "installing", statusInfoInstalling)
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
			})

			It("installing -> installing", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("with workers 1 in error, installing -> installing", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("with workers 2 in installing, installing -> installing", func() {
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("installing -> installing (some hosts are installed)", func() {
				createHost(id, "installing", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("installing -> installing (including installing-in-progress)", func() {
				createHost(id, "installing-in-progress", db)
				createHost(id, "installing-in-progress", db)
				createHost(id, "installing-in-progress", db)

				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("installing -> installing (including installing-in-progress)", func() {
				createHost(id, "installing-in-progress", db)
				createHost(id, "installing-in-progress", db)
				createHost(id, "installing", db)

				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("installing -> finalizing (kubeconfig not exist)", func() {
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)

				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
				shouldHaveUpdated = true
				expectedState = models.ClusterStatusFinalizing
			})
			It("with workers installing -> finalizing (kubeconfig not exist)", func() {
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "installed", db)
				createWorkerHost(id, "installed", db)

				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
				shouldHaveUpdated = true
				expectedState = models.ClusterStatusFinalizing
			})
			It("installing -> error", func() {
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", models.ClusterStatusInstalling, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				createHost(id, "error", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)

				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("installing -> error", func() {
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", models.ClusterStatusInstalling, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("installing -> error insufficient hosts", func() {
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", models.ClusterStatusInsufficient, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				createHost(id, "installing", db)
				createHost(id, "installed", db)
				createWorkerHost(id, "installed", db)
				shouldHaveUpdated = true
				expectedState = "error"

			})
			It("with workers in error, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with insufficient working workers count, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with insufficient working workers count, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with single worker in error, installing -> installing", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = false
				expectedState = "installing"
			})
			It("with single worker in error, installed -> finalizing", func() {
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createWorkerHost(id, "error", db)

				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
				shouldHaveUpdated = true
				expectedState = models.ClusterStatusFinalizing
			})
		})
		Context("from finalizing state", func() {
			BeforeEach(func() {
				c = createCluster(&id, models.ClusterStatusFinalizing, statusInfoFinalizing)
				createHost(id, models.HostStatusInstalled, db)
				createHost(id, models.HostStatusInstalled, db)
				createHost(id, models.HostStatusInstalled, db)
			})

			It("finalizing -> finalizing (kubeconfig not exist)", func() {
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing
			})

			It("finalizing -> finalizing (s3 failure)", func() {
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, errors.New("error")).Times(2)
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing
			})

			It("finalizing -> finalizing (kubeconfig exist, operator status empty)", func() {
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": ""}).Error).To(Not(HaveOccurred()))
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing
			})

			It("finalizing -> finalizing (kubeconfig exist, operator status processing)", func() {
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": models.OperatorStatusProgressing}).Error).To(Not(HaveOccurred()))
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing
			})

			It("finalizing -> finalizing (kubeconfig exist, operator status failure)", func() {
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing

				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), expectedState, models.ClusterStatusFinalizing, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": models.OperatorStatusFailed}).Error).To(Not(HaveOccurred()))
			})

			It("finalizing -> installed (kubeconfig exist, operator status available)", func() {
				shouldHaveUpdated = true
				expectedState = models.ClusterStatusInstalled
				mockMetric.EXPECT().MonitoredClusterCount(int64(0)).AnyTimes()
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), expectedState, models.ClusterStatusFinalizing, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": models.OperatorStatusAvailable}).Error).To(Not(HaveOccurred()))
			})
			It("finalizing -> finalizing, installing hosts exist", func() {
				createWorkerHost(id, models.HostStatusInstalled, db)
				createWorkerHost(id, models.HostStatusInstalled, db)
				createWorkerHost(id, models.HostStatusInstallingInProgress, db)
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusFinalizing
				mockMetric.EXPECT().MonitoredClusterCount(int64(0)).AnyTimes()
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), expectedState, models.ClusterStatusFinalizing, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": models.OperatorStatusAvailable}).Error).To(Not(HaveOccurred()))
			})
			It("finalizing -> installed, errored hosts exist", func() {
				createWorkerHost(id, models.HostStatusInstalled, db)
				createWorkerHost(id, models.HostStatusInstalled, db)
				createWorkerHost(id, models.HostStatusError, db)
				shouldHaveUpdated = true
				expectedState = models.ClusterStatusInstalled
				mockMetric.EXPECT().MonitoredClusterCount(int64(0)).AnyTimes()
				mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), expectedState, models.ClusterStatusFinalizing, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				Expect(db.Model(c.MonitoredOperators[0]).Updates(map[string]interface{}{"status": models.OperatorStatusAvailable}).Error).To(Not(HaveOccurred()))
			})
		})

		Context("from installed state", func() {
			BeforeEach(func() {
				c = createCluster(&id, models.ClusterStatusInstalled, statusInfoInstalled)
				mockMetric.EXPECT().MonitoredClusterCount(gomock.Any()).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			})

			It("installed -> installed", func() {
				createHost(id, models.HostStatusInstalled, db)
				createHost(id, models.HostStatusInstalled, db)
				createHost(id, models.HostStatusInstalled, db)
				shouldHaveUpdated = false
				expectedState = models.ClusterStatusInstalled
			})
		})

		mockHostAPIIsRequireUserActionResetFalse := func() {
			mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
		}

		Context("host hosts", func() {

			Context("from insufficient state", func() {
				BeforeEach(func() {

					c = common.Cluster{
						Cluster: models.Cluster{
							ID:              &id,
							Status:          swag.String("insufficient"),
							ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
							ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
							MachineNetworks: common.TestIPv4Networking.MachineNetworks,
							APIVips:         common.TestIPv4Networking.APIVips,
							IngressVips:     common.TestIPv4Networking.IngressVips,
							BaseDNSDomain:   "test.com",
							PullSecretSet:   true,
							StatusInfo:      swag.String(StatusInfoInsufficient),
							NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
						TriggerMonitorTimestamp: time.Now(),
					}

					Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
					Expect(err).ShouldNot(HaveOccurred())
				})

				It("insufficient -> insufficient", func() {
					createHost(id, "known", db)
					mockHostAPIIsRequireUserActionResetFalse()

					shouldHaveUpdated = false
					expectedState = "insufficient"
				})
				It("insufficient -> insufficient", func() {
					createHost(id, "insufficient", db)
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "known", db)
					mockHostAPIIsRequireUserActionResetFalse()
					shouldHaveUpdated = false
					expectedState = "insufficient"
				})
				It("insufficient -> ready", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "known", db)
					mockHostAPIIsRequireUserActionResetFalse()

					shouldHaveUpdated = true
					expectedState = "ready"

					err = network.UpdateVipsTables(db,
						&common.Cluster{Cluster: models.Cluster{
							ID:          c.ID,
							APIVips:     []*models.APIVip{{IP: "1.2.3.5", ClusterID: *c.ID}},
							IngressVips: []*models.IngressVip{{IP: "1.2.3.6", ClusterID: *c.ID}},
						}},
						true,
						true,
					)
					Expect(err).ShouldNot(HaveOccurred())
				})
				It("insufficient -> insufficient including hosts in discovering", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "discovering", db)
					mockHostAPIIsRequireUserActionResetFalse()

					shouldHaveUpdated = false
					expectedState = "insufficient"
				})
				It("insufficient -> insufficient including hosts in error", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "error", db)
					mockHostAPIIsRequireUserActionResetFalse()

					shouldHaveUpdated = false
					expectedState = "insufficient"
				})
				It("insufficient -> insufficient including hosts in disabled", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "disabled", db)
					mockHostAPIIsRequireUserActionResetFalse()

					shouldHaveUpdated = false
					expectedState = "insufficient"
				})
			})
			Context("from ready state", func() {
				BeforeEach(func() {
					c = common.Cluster{
						Cluster: models.Cluster{
							ID:              &id,
							Status:          swag.String(models.ClusterStatusReady),
							StatusInfo:      swag.String(StatusInfoReady),
							ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
							ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
							MachineNetworks: common.TestIPv4Networking.MachineNetworks,
							APIVips:         common.TestIPv4Networking.APIVips,
							IngressVips:     common.TestIPv4Networking.IngressVips,
							BaseDNSDomain:   "test.com",
							PullSecretSet:   true,
							NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
						TriggerMonitorTimestamp: time.Now(),
					}

					Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				})

				It("ready -> ready", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "known", db)

					shouldHaveUpdated = false
					expectedState = "ready"
				})
				It("ready -> insufficient", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)

					shouldHaveUpdated = true
					expectedState = "insufficient"
				})
				It("ready -> insufficient one host is discovering", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "discovering", db)

					shouldHaveUpdated = true
					expectedState = "insufficient"
				})
				It("ready -> insufficient including hosts in error", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "error", db)

					shouldHaveUpdated = true
					expectedState = "insufficient"
				})
				It("ready -> insufficient including hosts in disabled", func() {
					createHost(id, "known", db)
					createHost(id, "known", db)
					createHost(id, "disabled", db)

					shouldHaveUpdated = true
					expectedState = "insufficient"
				})
			})

		})

		AfterEach(func() {
			before := time.Now().Truncate(10 * time.Millisecond)
			c = getClusterFromDB(id, db)
			saveUpdatedTime := c.StatusUpdatedAt
			saveStatusInfo := c.StatusInfo
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithClusterIdMatcher(c.ID.String()))).AnyTimes()
			mockHostAPIIsRequireUserActionResetFalse()
			clusterApi.ClusterMonitoring()
			after := time.Now().Truncate(10 * time.Millisecond)
			c = getClusterFromDB(id, db)
			Expect(swag.StringValue(c.Status)).Should(Equal(expectedState))
			if shouldHaveUpdated {
				Expect(c.StatusInfo).ShouldNot(BeNil())
				updateTime := time.Time(c.StatusUpdatedAt).Truncate(10 * time.Millisecond)
				Expect(updateTime).Should(BeTemporally(">=", before))
				Expect(updateTime).Should(BeTemporally("<=", after))

				installationCompletedStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError, models.ClusterStatusCancelled}
				if funk.ContainsString(installationCompletedStatuses, expectedState) {
					Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
				}
			} else {
				Expect(c.StatusUpdatedAt).Should(Equal(saveUpdatedTime))
				Expect(c.StatusInfo).Should(Equal(saveStatusInfo))
			}

			preSecondRefreshUpdatedTime := c.UpdatedAt
			clusterApi.ClusterMonitoring()
			c = getClusterFromDB(id, db)
			postRefreshUpdateTime := c.UpdatedAt
			Expect(preSecondRefreshUpdatedTime).Should(Equal(postRefreshUpdateTime))
		})
	})

	Context("batch", func() {

		monitorKnownToInsufficient := func(nClusters int) {
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).AnyTimes()
			mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
			mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(true, nil).AnyTimes()

			for i := 0; i < nClusters; i++ {
				id = strfmt.UUID(uuid.New().String())
				c = common.Cluster{Cluster: models.Cluster{
					ID:              &id,
					Status:          swag.String(models.ClusterStatusReady),
					StatusInfo:      swag.String(StatusInfoReady),
					ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
					ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
					MachineNetworks: common.TestIPv4Networking.MachineNetworks,
					APIVips:         common.TestIPv4Networking.APIVips,
					IngressVips:     common.TestIPv4Networking.IngressVips,
					BaseDNSDomain:   "test.com",
					PullSecretSet:   true,
				}}
				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())

				createHost(id, "known", db)
				createHost(id, "known", db)

			}

			clusterApi.ClusterMonitoring()

			var count int64
			err = db.Model(&common.Cluster{}).Where("status = ?", models.ClusterStatusInsufficient).
				Count(&count).Error
			Expect(err).ShouldNot(HaveOccurred())
			Expect(count).Should(Equal(int64(nClusters)))
		}

		It("10 clusters monitor", func() {
			mockMetric.EXPECT().MonitoredClusterCount(int64(10)).Times(1)
			monitorKnownToInsufficient(10)
		})

		It("352 clusters monitor", func() {
			mockMetric.EXPECT().MonitoredClusterCount(int64(352)).Times(1)
			monitorKnownToInsufficient(352)
		})
	})

	Context("monitoring log info", func() {
		Context("error -> error", func() {
			var (
				ctx = context.Background()
			)

			BeforeEach(func() {
				c = common.Cluster{Cluster: models.Cluster{
					ID:              &id,
					Status:          swag.String("error"),
					StatusInfo:      swag.String(statusInfoError),
					MachineNetworks: common.TestIPv4Networking.MachineNetworks,
					BaseDNSDomain:   "test.com",
					PullSecretSet:   true,
				}}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName))).Times(0)
				mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).Times(0)
				mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(0)
			})

			It("empty log info (no logs expected or arrived)", func() {
				clusterApi.ClusterMonitoring()
				c = getClusterFromDB(id, db)
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusError))
				Expect(c.LogsInfo).Should(Equal(models.LogsStateTimeout))
			})
			It("log requested", func() {
				progress := models.LogsStateRequested
				_ = clusterApi.UpdateLogsProgress(ctx, &c, string(progress))
				clusterApi.ClusterMonitoring()
				c = getClusterFromDB(id, db)
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusError))
				Expect(c.LogsInfo).Should(Equal(progress))
			})
		})

	})
})

var _ = Describe("lease timeout event", func() {
	var (
		db                 *gorm.DB
		c                  common.Cluster
		id                 strfmt.UUID
		clusterApi         *Manager
		ctrl               *gomock.Controller
		mockHostAPI        *host.MockAPI
		mockMetric         *metrics.MockAPI
		dbName             string
		mockEvents         *eventsapi.MockHandler
		mockEventsUploader *uploader.MockClient
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, mockEventsUploader, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil, nil)

		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name                string
		srcState            string
		apiVip              string
		ingressVip          string
		shouldTimeout       bool
		eventCalllsExpected int
	}{
		{
			name:                "VIPs exist",
			srcState:            models.ClusterStatusReady,
			apiVip:              "1.2.3.4",
			ingressVip:          "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 2,
		},
		{
			name:                "API Vip missing with timeout",
			srcState:            models.ClusterStatusReady,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 2,
		},
		{
			name:                "API Vip missing without timeout",
			srcState:            models.ClusterStatusReady,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       false,
			eventCalllsExpected: 1,
		},
		{
			name:                "API Vip missing without timeout from insufficient",
			srcState:            models.ClusterStatusInsufficient,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       false,
			eventCalllsExpected: 0,
		},
		{
			name:                "Ingress Vip missing with timeout from insufficient",
			srcState:            models.ClusterStatusInsufficient,
			apiVip:              "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 1,
		},
	}
	for _, t := range tests {
		t := t
		It(t.name, func() {
			c = common.Cluster{Cluster: models.Cluster{
				ID:                &id,
				Status:            swag.String(t.srcState),
				ClusterNetworks:   common.TestIPv4Networking.ClusterNetworks,
				ServiceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
				BaseDNSDomain:     "test.com",
				PullSecretSet:     true,
				VipDhcpAllocation: swag.Bool(true),
			}}
			if t.shouldTimeout {
				c.MachineNetworkCidrUpdatedAt = time.Now().Add(-2 * time.Minute)
			} else {
				c.MachineNetworkCidrUpdatedAt = time.Now().Add(-1 * time.Minute)
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			if t.eventCalllsExpected > 0 {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithClusterIdMatcher(c.ID.String()))).Times(t.eventCalllsExpected)
			}
			clusterApi.ClusterMonitoring()
			ctrl.Finish()
		})
	}
})

var _ = Describe("Auto assign machine CIDR", func() {
	var (
		db                 *gorm.DB
		c                  common.Cluster
		id                 strfmt.UUID
		clusterApi         *Manager
		ctrl               *gomock.Controller
		mockHostAPI        *host.MockAPI
		mockMetric         *metrics.MockAPI
		dbName             string
		mockEvents         *eventsapi.MockHandler
		mockEventsUploader *uploader.MockClient
		defaultIPv4Address = common.NetAddress{IPv4Address: []string{"1.2.3.0/24"}}
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, mockEventsUploader, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil, nil)

		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name                    string
		srcState                string
		expectedMachineCIDR     string
		expectedMachineNetworks []string
		apiVip                  string
		apiVips                 []*models.APIVip
		hosts                   []*models.Host
		eventCallExpected       bool
		userActionResetExpected bool
		dhcpEnabled             bool
		userManagedNetworking   bool
		sno                     bool
		clusterNetworks         []*models.ClusterNetwork
		serviceNetworks         []*models.ServiceNetwork
		machineNetworks         []*models.MachineNetwork
	}{
		{
			name:        "No hosts",
			srcState:    models.ClusterStatusPendingForInput,
			dhcpEnabled: true,
		},
		{
			name:     "One discovering host",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusDiscovering),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			dhcpEnabled: true,
		},
		{
			name:     "One insufficient host, one network",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             true,
		},
		{
			name:     "Host with two networks",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: twoNetworksInventory(),
				},
			},
			dhcpEnabled: true,
		},
		{
			name:     "Two hosts, one networks",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             true,
		},
		{
			name:     "Two hosts, two networks",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: nonDefaultInventory(),
				},
			},
			dhcpEnabled: true,
		},
		{
			name:     "One insufficient host, one network, machine cidr already set",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}},
			expectedMachineCIDR:     "192.168.0.0/16",
			dhcpEnabled:             true,
		},
		{
			name:     "Two hosts, one networks, dhcp disabled, no vips",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			dhcpEnabled:             false,
		},
		{
			name:        "No hosts - dhcp disabled",
			srcState:    models.ClusterStatusPendingForInput,
			dhcpEnabled: false,
			apiVip:      "1.2.3.8",
			apiVips:     []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One discovering host - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status: swag.String(models.HostStatusDiscovering),
				},
			},
			dhcpEnabled: false,
			apiVip:      "1.2.3.8",
			apiVips:     []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One insufficient host, one network - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "Host with two networks - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: twoNetworksInventory(),
				},
			},
			dhcpEnabled:         false,
			apiVip:              "1.2.3.8",
			apiVips:             []*models.APIVip{{IP: models.IP("1.2.3.8")}},
			expectedMachineCIDR: "1.2.3.0/24",
		},
		{
			name:     "Two hosts, one networks - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			dhcpEnabled:             false,
			expectedMachineCIDR:     "1.2.3.0/24",
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "Two hosts, one networks - dhcp disabled, user managed networking",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
			userManagedNetworking:   true,
		},
		{
			name:     "Two hosts, one networks - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestInventoryWithNetwork(defaultIPv4Address),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "Two hosts, two networks - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusPendingForInput),
					Inventory: nonDefaultInventory(),
				},
			},
			dhcpEnabled:         false,
			expectedMachineCIDR: "1.2.3.0/24",
			apiVip:              "1.2.3.8",
			apiVips:             []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One insufficient host, one network, different machine cidr already set - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}},
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One insufficient host, one network, same machine cidr already set - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("1.2.3.0/24")}},
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:                    "No hosts, machine cidr already set - dhcp disabled",
			srcState:                models.ClusterStatusPendingForInput,
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}},
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One insufficient host, no networks, machine cidr already set - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status: swag.String(models.HostStatusInsufficient),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}},
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
			apiVips:                 []*models.APIVip{{IP: models.IP("1.2.3.8")}},
		},
		{
			name:     "One insufficient host, one network, machine cidr already set, no vips - dhcp disabled",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworks:         []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}},
			dhcpEnabled:             false,
		},
		{
			name:     "Pending SNO IPv4",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			sno:                 true,
			expectedMachineCIDR: "1.2.3.0/24",
			expectedMachineNetworks: []string{
				"1.2.3.0/24",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending SNO IPv6",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			sno:                 true,
			clusterNetworks:     common.TestIPv6Networking.ClusterNetworks,
			serviceNetworks:     common.TestIPv6Networking.ServiceNetworks,
			expectedMachineCIDR: "1001:db8::/120",
			expectedMachineNetworks: []string{
				"1001:db8::/120",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending SNO Dual stack",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			sno:                 true,
			clusterNetworks:     common.TestDualStackNetworking.ClusterNetworks,
			serviceNetworks:     common.TestDualStackNetworking.ServiceNetworks,
			expectedMachineCIDR: "1.2.3.0/24",
			expectedMachineNetworks: []string{
				"1.2.3.0/24",
				"1001:db8::/120",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending multi-node dual-stack with dual-stack VIPs - autocalculation of machine networks",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			clusterNetworks:     common.TestDualStackNetworking.ClusterNetworks,
			serviceNetworks:     common.TestDualStackNetworking.ServiceNetworks,
			expectedMachineCIDR: "1.2.3.0/24",
			expectedMachineNetworks: []string{
				"1.2.3.0/24",
				"1001:db8::/120",
			},
			apiVip:      "1.2.3.8",
			apiVips:     []*models.APIVip{{IP: models.IP("1.2.3.8")}, {IP: models.IP("1001:db8::8")}},
			dhcpEnabled: false,
		},
		{
			name:                "No hosts, dual-stack - don't remove machine networks",
			srcState:            models.ClusterStatusPendingForInput,
			clusterNetworks:     common.TestIPv6Networking.ClusterNetworks,
			serviceNetworks:     common.TestIPv6Networking.ServiceNetworks,
			machineNetworks:     []*models.MachineNetwork{{Cidr: models.Subnet("192.168.0.0/16")}, {Cidr: models.Subnet("1001:db8::/120")}},
			expectedMachineCIDR: "192.168.0.0/16",
			expectedMachineNetworks: []string{
				"192.168.0.0/16",
				"1001:db8::/120",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending SNO IPv4 2 addresses",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTest2IPv4AddressesInventory(),
				},
			},
			sno:                 true,
			expectedMachineCIDR: "1.2.3.0/24",
			expectedMachineNetworks: []string{
				"1.2.3.0/24",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending SNO IPv6",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTest2IPv4AddressesInventory(),
				},
			},
			sno:                 true,
			clusterNetworks:     common.TestIPv6Networking.ClusterNetworks,
			serviceNetworks:     common.TestIPv6Networking.ServiceNetworks,
			expectedMachineCIDR: "1001:db8::/120",
			expectedMachineNetworks: []string{
				"1001:db8::/120",
			},
			dhcpEnabled: false,
		},
		{
			name:     "Pending SNO conflicting service/cluster networks",
			srcState: models.ClusterStatusPendingForInput,
			hosts: []*models.Host{
				{
					Status:    swag.String(models.HostStatusInsufficient),
					Inventory: common.GenerateTestDefaultInventory(),
				},
			},
			sno:                     true,
			serviceNetworks:         common.TestIPv6Networking.ServiceNetworks,
			expectedMachineCIDR:     "",
			expectedMachineNetworks: []string{},
			dhcpEnabled:             false,
		},
	}
	for _, t := range tests {
		t := t
		It(t.name, func() {
			c = common.Cluster{Cluster: models.Cluster{
				ID:                    &id,
				Status:                swag.String(t.srcState),
				BaseDNSDomain:         "test.com",
				PullSecretSet:         true,
				APIVips:               t.apiVips,
				ClusterNetworks:       common.TestIPv4Networking.ClusterNetworks,
				ServiceNetworks:       common.TestIPv4Networking.ServiceNetworks,
				MachineNetworks:       t.machineNetworks,
				VipDhcpAllocation:     swag.Bool(t.dhcpEnabled),
				UserManagedNetworking: swag.Bool(t.userManagedNetworking),
			}}
			if t.sno {
				c.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
				c.UserManagedNetworking = swag.Bool(true)
			}
			if t.clusterNetworks != nil {
				c.ClusterNetworks = t.clusterNetworks
			}
			if t.serviceNetworks != nil {
				c.ServiceNetworks = t.serviceNetworks
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			for _, h := range t.hosts {
				hostId := strfmt.UUID(uuid.New().String())
				h.ID = &hostId
				h.InfraEnvID = id
				h.ClusterID = &id
				Expect(db.Create(h).Error).ShouldNot(HaveOccurred())
			}
			if t.eventCallExpected {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithClusterIdMatcher(c.ID.String()))).AnyTimes()
			}
			if len(t.hosts) > 0 {
				mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			}
			if t.userActionResetExpected {
				mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).AnyTimes()
			}
			clusterApi.ClusterMonitoring()
			cluster := getClusterFromDB(id, db)
			if t.expectedMachineCIDR == "" {
				Expect(cluster.MachineNetworks).To(BeEmpty())
				Expect(cluster.MachineNetworkCidr).To(Equal("")) // TODO(MGMT-9751-remove-single-network)
			} else {
				Expect(cluster.MachineNetworks).NotTo(BeEmpty())
				Expect(network.GetMachineCidrById(&cluster, 0)).To(Equal(t.expectedMachineCIDR))
				Expect(cluster.MachineNetworkCidr).To(Equal("")) // TODO(MGMT-9751-remove-single-network)
			}
			if t.expectedMachineNetworks != nil {
				Expect(cluster.MachineNetworks).To(HaveLen(len(t.expectedMachineNetworks)))
				for i, cidr := range t.expectedMachineNetworks {
					Expect(string(cluster.MachineNetworks[i].Cidr)).To(Equal(cidr))
				}
			}

			ctrl.Finish()
		})
	}
})

var _ = Describe("VerifyRegisterHost", func() {
	var (
		db                     *gorm.DB
		id                     strfmt.UUID
		clusterApi             *Manager
		preInstalledError      string = "Host can register only in one of the following states: [insufficient ready pending-for-input adding-hosts]"
		postInstalledErrorSaas string = "Cannot add hosts to an existing cluster using the original Discovery ISO. Try to add new hosts by using the Discovery ISO that can be found in console.redhat.com under your cluster “Add hosts“ tab."
		postInstalledError     string = "Cannot add hosts to an existing cluster using the original Discovery ISO."
		dbName                 string
		ctrl                   *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			nil, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	checkVerifyRegisterHost := func(clusterStatus string, expectErr bool, errTemplate string) {
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(clusterStatus)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = getClusterFromDB(id, db)
		err := clusterApi.AcceptRegistration(&cluster)
		if expectErr {
			Expect(err.Error()).Should(Equal(errors.Errorf(errTemplate).Error()))
		} else {
			Expect(err).Should(BeNil())
		}
	}
	It("Register host while cluster in ready state", func() {
		checkVerifyRegisterHost(models.ClusterStatusReady, false, preInstalledError)
	})
	It("Register host while cluster in insufficient state", func() {
		checkVerifyRegisterHost(models.ClusterStatusInsufficient, false, preInstalledError)
	})
	It("Register host while cluster in installing state", func() {
		checkVerifyRegisterHost(models.ClusterStatusInstalling, true, preInstalledError)
	})
	It("Register host while cluster in finallizing state", func() {
		checkVerifyRegisterHost(models.ClusterStatusFinalizing, true, preInstalledError)
	})
	It("Register host while cluster in error state", func() {
		checkVerifyRegisterHost(models.ClusterStatusError, true, preInstalledError)
	})

	It("Register host while cluster in installed state - SAAS", func() {
		clusterApi.authHandler = &auth.RHSSOAuthenticator{}
		checkVerifyRegisterHost(models.ClusterStatusInstalled, true, postInstalledErrorSaas)
	})

	It("Register host while cluster in installed state", func() {
		clusterApi.authHandler = &auth.NoneAuthenticator{}
		checkVerifyRegisterHost(models.ClusterStatusInstalled, true, postInstalledError)
	})
})

var _ = Describe("VerifyClusterUpdatability", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, cluster can be updated only in one of [insufficient ready pending-for-input adding-hosts]"
		dbName      string
		ctrl        *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			nil, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	checkVerifyClusterUpdatability := func(clusterStatus string, expectErr bool) {
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(clusterStatus)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = getClusterFromDB(id, db)
		err := clusterApi.VerifyClusterUpdatability(&cluster)
		if expectErr {
			Expect(err.Error()).Should(Equal(errors.Errorf(errTemplate, id, clusterStatus).Error()))
		} else {
			Expect(err).Should(BeNil())
		}
	}
	It("Update cluster while insufficient", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInsufficient, false)
	})
	It("Update cluster while ready", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusReady, false)
	})
	It("Update cluster while installing", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInstalling, true)
	})
	It("Update cluster while installed", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInstalled, true)
	})
	It("Update cluster while error", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusError, true)
	})
})

var _ = Describe("CancelInstallation", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler eventsapi.Handler
		ctrl          *gomock.Controller
		mockMetric    *metrics.MockAPI
		dbName        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     swag.String(models.ClusterStatusInsufficient),
			StatusInfo: swag.String(StatusInfoInsufficient)}}
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			c.Status = swag.String(models.ClusterStatusInstalling)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusCancelled, models.ClusterStatusInstalling, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(c.ID, nil, nil))
			Expect(err).ShouldNot(HaveOccurred())
			events := response.GetEvents()
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := eventstest.FindEventByName(events, eventgen.ClusterInstallationCanceledEventName)
			Expect(cancelEvent).NotTo(BeNil())
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			Expect(*cancelEvent.Message).Should(Equal("Canceled cluster installation"))
		})

		It("cancel_failed_installation", func() {
			c.Status = swag.String(models.ClusterStatusError)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(c.ID, nil, nil))
			Expect(err).ShouldNot(HaveOccurred())
			events := response.GetEvents()
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := eventstest.FindEventByName(events, eventgen.ClusterInstallationCanceledEventName)
			Expect(cancelEvent).NotTo(BeNil())
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			Expect(*cancelEvent.Message).Should(Equal("Canceled cluster installation"))
		})

		AfterEach(func() {
			db.First(&c, "id = ?", c.ID)
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
		})
	})

	Context("invalid_cancel_installation", func() {
		It("nothing_to_cancel", func() {
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).Should(HaveOccurred())
			response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(c.ID, nil, nil))
			Expect(err).ShouldNot(HaveOccurred())
			events := response.GetEvents()
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := eventstest.FindEventByName(events, eventgen.CancelInstallationFailedEventName)
			Expect(cancelEvent).NotTo(BeNil())
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})
})

var _ = Describe("ResetCluster", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler eventsapi.Handler
		dbName        string
		ctrl          *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("reset_cluster", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:                 &id,
			Status:             swag.String(models.ClusterStatusError),
			OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		Expect(state.ResetCluster(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
		db.First(&c, "id = ?", c.ID)
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
		response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(c.ID, nil, nil))
		Expect(err).ShouldNot(HaveOccurred())
		events := response.GetEvents()
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := eventstest.FindEventByName(events, eventgen.ClusterInstallationResetEventName)
		Expect(resetEvent).NotTo(BeNil())
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
		Expect(*resetEvent.Message).Should(Equal("Reset cluster installation"))

		count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
		Expect(count).To(Equal(int64(0)))
	})

	It("reset cluster conflict", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusReady),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		reply := state.ResetCluster(ctx, &c, "some reason", db)
		Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
		response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(c.ID, nil, nil))
		Expect(err).ShouldNot(HaveOccurred())
		events := response.GetEvents()
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := eventstest.FindEventByName(events, eventgen.ResetInstallationFailedEventName)
		Expect(resetEvent).NotTo(BeNil())
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityError))
	})
})

func createHost(clusterId strfmt.UUID, state string, db *gorm.DB) {
	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: clusterId,
		ClusterID:  &clusterId,
		Role:       models.HostRoleMaster,
		Status:     swag.String(state),
		Inventory:  common.GenerateTestDefaultInventory(),
		FreeAddresses: makeFreeNetworksAddressesStr(
			makeFreeAddresses(
				string(common.TestIPv4Networking.MachineNetworks[0].Cidr),
				strfmt.IPv4(common.TestIPv4Networking.IngressVips[0].IP),
				strfmt.IPv4(common.TestIPv4Networking.APIVips[0].IP),
			),
		),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func createWorkerHost(clusterId strfmt.UUID, state string, db *gorm.DB) {
	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: clusterId,
		ClusterID:  &clusterId,
		Role:       models.HostRoleWorker,
		Status:     swag.String(state),
		Inventory:  common.GenerateTestDefaultInventory(),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func addInstallationRequirements(clusterId strfmt.UUID, db *gorm.DB) {
	var hostId strfmt.UUID
	var host models.Host
	for i := 0; i < 3; i++ {
		hostId = strfmt.UUID(uuid.New().String())
		host = models.Host{
			ID:         &hostId,
			InfraEnvID: clusterId,
			ClusterID:  &clusterId,
			Role:       models.HostRoleMaster,
			Status:     swag.String("known"),
			Inventory:  common.GenerateTestDefaultInventory(),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

	}
}

func addInstallationRequirementsWithConnectivity(clusterId strfmt.UUID, db *gorm.DB, remoteIpAddresses ...string) {
	var hostId strfmt.UUID
	var host models.Host
	hostIds := []strfmt.UUID{
		strfmt.UUID(uuid.New().String()),
		strfmt.UUID(uuid.New().String()),
		strfmt.UUID(uuid.New().String()),
	}
	for i := 0; i < 3; i++ {
		hostId = hostIds[i]
		otherIds := []strfmt.UUID{
			hostIds[(i+1)%3],
			hostIds[(i+2)%3],
		}
		makeL2Connectivity := func() []*models.L2Connectivity {
			ret := make([]*models.L2Connectivity, 0)
			for _, r := range remoteIpAddresses {
				ret = append(ret, &models.L2Connectivity{
					Successful:      true,
					RemoteIPAddress: r,
				})
			}
			return ret
		}
		connectivityReport := models.ConnectivityReport{
			RemoteHosts: []*models.ConnectivityRemoteHost{
				{
					HostID:         otherIds[0],
					L2Connectivity: makeL2Connectivity(),
				},
				{
					HostID:         otherIds[1],
					L2Connectivity: makeL2Connectivity(),
				},
			},
		}
		b, err := json.Marshal(&connectivityReport)
		Expect(err).ToNot(HaveOccurred())
		host = models.Host{
			ID:           &hostId,
			InfraEnvID:   clusterId,
			ClusterID:    &clusterId,
			Role:         models.HostRoleMaster,
			Status:       swag.String("known"),
			Inventory:    twoNetworksInventory(),
			Connectivity: string(b),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

	}
}

func defaultInventory() string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		CPU: &models.CPU{
			Count: 16,
		},
		Memory: &models.Memory{
			UsableBytes: 64000000000,
		},
		Disks: []*models.Disk{
			{
				SizeBytes: 20000000000,
				DriveType: models.DriveTypeHDD,
			}, {
				SizeBytes: 40000000000,
				DriveType: models.DriveTypeSSD,
			},
		},
		Routes: common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func twoNetworksInventory() string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
					"10.0.0.100/24",
				},
			},
		},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func nonDefaultInventory() string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"10.11.12.13/24",
				},
			},
		},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("PrepareForInstallation", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
		dbName    string
		ctrl      *gomock.Controller

		mockMetric        *metrics.MockAPI
		mockEventsHandler *eventsapi.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEventsHandler = eventsapi.NewMockHandler(ctrl)
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEventsHandler, nil, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	// state changes to preparing-for-installation
	success := func(cluster *common.Cluster) {
		mockEventsHandler.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		Expect(capi.PrepareForInstallation(ctx, cluster, db)).NotTo(HaveOccurred())
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).To(Equal(models.ClusterStatusPreparingForInstallation))
		Expect(time.Time(cluster.ControllerLogsCollectedAt).Equal(time.Time{})).To(BeTrue())
	}

	// status should not change
	failure := func(cluster *common.Cluster) {
		src := swag.StringValue(cluster.Status)
		Expect(capi.PrepareForInstallation(ctx, cluster, db)).To(HaveOccurred())
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(src))
		Expect(cluster.ControllerLogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
	}

	tests := []struct {
		name       string
		srcState   string
		validation func(cluster *common.Cluster)
	}{
		{
			name:       "success from ready",
			srcState:   models.ClusterStatusReady,
			validation: success,
		},
		{
			name:       "already prepared for installation - should fail",
			srcState:   models.ClusterStatusPreparingForInstallation,
			validation: failure,
		},
		{
			name:       "insufficient - should fail",
			srcState:   models.ClusterStatusInsufficient,
			validation: failure,
		},
		{
			name:       "installing - should fail",
			srcState:   models.ClusterStatusInstalling,
			validation: failure,
		},
		{
			name:       "error - should fail",
			srcState:   models.ClusterStatusError,
			validation: failure,
		},
		{
			name:       "installed - should fail",
			srcState:   models.ClusterStatusInstalled,
			validation: failure,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                        &clusterId,
					Status:                    swag.String(t.srcState),
					ControllerLogsCollectedAt: strfmt.DateTime(time.Now()),
				}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Take(&cluster, "id = ?", clusterId).Error).ShouldNot(HaveOccurred())
			t.validation(&cluster)
		})
	}
})

var _ = Describe("HandlePreInstallationChanges", func() {
	var (
		ctx        = context.Background()
		capi       API
		db         *gorm.DB
		clusterId  strfmt.UUID
		dbName     string
		mockEvents *eventsapi.MockHandler
		ctrl       *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		cluster := &common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusPreparingForInstallation)}}
		Expect(db.Create(cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("HandlePreInstallError", func() {
		var cluster models.Cluster
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		commonCluster := common.Cluster{}
		commonCluster.ID = cluster.ID
		capi.HandlePreInstallError(ctx, &commonCluster, errors.Errorf("pre-install error"))
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(cluster.LastInstallationPreparation.Status).Should(Equal(models.LastInstallationPreparationStatusFailed))
	})

	It("HandlePreInstallSuccess", func() {
		var cluster models.Cluster
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		commonCluster := common.Cluster{}
		commonCluster.ID = cluster.ID
		capi.HandlePreInstallSuccess(ctx, &commonCluster)
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(cluster.LastInstallationPreparation.Status).Should(Equal(models.LastInstallationPreparationStatusSuccess))
	})
})

var _ = Describe("SetVipsData", func() {
	var (
		ctx        = context.Background()
		capi       API
		mockEvents *eventsapi.MockHandler
		ctrl       *gomock.Controller
		db         *gorm.DB
		clusterId  strfmt.UUID
		dbName     string
		apiLease   = `lease {
  interface "api";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}`
		expectedApiLease = `lease {
  interface "api";
  renew never;
  rebind never;
  expire never;
}`
		ingressLease = `lease {
  interface "ingress";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}`
		expectedIngressLease = `lease {
  interface "ingress";
  renew never;
  rebind never;
  expire never;
}`
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name                 string
		srcState             string
		clusterApiVip        string
		clusterIngressVip    string
		clusterApiLease      string
		clusterIngressLease  string
		apiVip               string
		ingressVip           string
		expectedApiVip       string
		expectedIngressVip   string
		expectedApiLease     string
		expectedIngressLease string
		expectedState        string
		errorExpected        bool
		eventExpected        bool
	}{
		{
			name:               "success-empty",
			srcState:           models.ClusterStatusInsufficient,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInsufficient,
			eventExpected:      true,
		},
		{
			name:               "success-empty, from pending-from-input",
			srcState:           models.ClusterStatusPendingForInput,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusPendingForInput,
			eventExpected:      true,
		},
		{
			name:               "success-empty from ready",
			srcState:           models.ClusterStatusReady,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusReady,
			eventExpected:      true,
		},
		{
			name:               "success- insufficient",
			srcState:           models.ClusterStatusInsufficient,
			clusterApiVip:      "1.1.1.1",
			clusterIngressVip:  "2.2.2.2",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInsufficient,
			eventExpected:      true,
		},
		{
			name:                 "success- insufficient with leases",
			srcState:             models.ClusterStatusInsufficient,
			clusterApiVip:        "1.1.1.1",
			clusterIngressVip:    "2.2.2.2",
			clusterApiLease:      apiLease,
			clusterIngressLease:  ingressLease,
			apiVip:               "1.2.3.4",
			ingressVip:           "1.2.3.5",
			expectedApiVip:       "1.2.3.4",
			expectedIngressVip:   "1.2.3.5",
			expectedApiLease:     expectedApiLease,
			expectedIngressLease: expectedIngressLease,
			errorExpected:        false,
			expectedState:        models.ClusterStatusInsufficient,
			eventExpected:        true,
		},
		{
			name:               "success- ready same",
			srcState:           models.ClusterStatusReady,
			clusterApiVip:      "1.2.3.4",
			clusterIngressVip:  "1.2.3.5",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusReady,
		},
		{
			name:               "failure- installing",
			srcState:           models.ClusterStatusInstalling,
			clusterApiVip:      "1.1.1.1",
			clusterIngressVip:  "2.2.2.2",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.1.1.1",
			expectedIngressVip: "2.2.2.2",
			errorExpected:      true,
			expectedState:      models.ClusterStatusInstalling,
		},
		{
			name:               "success- ready same",
			srcState:           models.ClusterStatusInstalling,
			clusterApiVip:      "1.2.3.4",
			clusterIngressVip:  "1.2.3.5",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInstalling,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:          &clusterId,
					Status:      swag.String(t.srcState),
					APIVips:     []*models.APIVip{{IP: models.IP(t.clusterApiVip), ClusterID: clusterId}},
					IngressVips: []*models.IngressVip{{IP: models.IP(t.clusterIngressVip), ClusterID: clusterId}},
				},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			if t.eventExpected {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ApiIngressVipUpdatedEventName),
					eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
			}
			err := capi.SetVipsData(ctx, &cluster, t.apiVip, t.ingressVip, t.clusterApiLease, t.clusterIngressLease, db)
			Expect(err != nil).To(Equal(t.errorExpected))
			c := getClusterFromDB(clusterId, db)
			Expect(network.GetApiVipById(&c, 0)).To(Equal(t.expectedApiVip))
			Expect(len(c.APIVips)).To(Equal(1))
			Expect(len(c.IngressVips)).To(Equal(1))
			Expect(network.GetIngressVipById(&c, 0)).To(Equal(t.expectedIngressVip))
			Expect(c.ApiVipLease).To(Equal(t.expectedApiLease))
			Expect(c.IngressVipLease).To(Equal(t.expectedIngressLease))
			Expect(swag.StringValue(c.Status)).To(Equal(t.expectedState))
		})
	}
})

var _ = Describe("Majority groups", func() {
	var (
		dbIndex       int
		clusterApi    *Manager
		db            *gorm.DB
		id            strfmt.UUID
		cluster       common.Cluster
		ctrl          *gomock.Controller
		mockEvents    *eventsapi.MockHandler
		mockMetricApi *metrics.MockAPI
		dbName        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dbIndex++
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		mockMetricApi = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, nil, nil, mockMetricApi, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"
		verificationSuccess := models.VipVerificationSucceeded
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(models.ClusterStatusReady),
			ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips:         []*models.APIVip{{IP: models.IP(apiVip), ClusterID: id, Verification: &verificationSuccess}},
			IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: id, Verification: &verificationSuccess}},
			BaseDNSDomain:   "test.com",
			PullSecretSet:   true,
			NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		mockMetricApi.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockMetricApi.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	setup := func(ips []string) {
		addInstallationRequirementsWithConnectivity(id, db, ips...)

		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusReady))
		Expect(len(cluster.Hosts)).Should(Equal(3))
	}
	expect := func(networks ...string) {
		c := getClusterFromDB(*cluster.ID, db)
		majorityGroups := make(map[string][]strfmt.UUID)
		Expect(json.Unmarshal([]byte(c.ConnectivityMajorityGroups), &majorityGroups)).ToNot(HaveOccurred())
		Expect(majorityGroups).ToNot(BeEmpty())
		for _, n := range []string{"1.2.3.0/24", "10.0.0.0/24"} {
			group := majorityGroups[n]
			if funk.ContainsString(networks, n) {
				for _, h := range cluster.Hosts {
					Expect(group).To(ContainElement(*h.ID))
				}
			} else {
				Expect(group).To(BeEmpty())
			}
		}
	}
	Context("Set Majority groups", func() {
		run := func(ips ...string) {
			setup(ips)
			Expect(clusterApi.SetConnectivityMajorityGroupsForCluster(id, db)).ToNot(HaveOccurred())
		}
		It("Empty", func() {
			run()
			expect()
		})
		It("Success", func() {
			run("1.2.3.10")
			expect("1.2.3.0/24")
		})
		It("Two networks", func() {
			run("1.2.3.10", "10.0.0.15")
			expect("1.2.3.0/24", "10.0.0.0/24")
		})
	})
	Context("monitoring", func() {
		run := func(ips ...string) {
			setup(ips)
			clusterApi.ClusterMonitoring()
		}
		It("Empty", func() {
			run()
			expect()
		})
		It("Success", func() {
			run("1.2.3.10")
			expect("1.2.3.0/24")
		})
		It("Two networks", func() {
			run("1.2.3.10", "10.0.0.15")
			expect("1.2.3.0/24", "10.0.0.0/24")
		})
	})
})

var _ = Describe("validate vips response", func() {
	var (
		ctx                                            = context.Background()
		clusterApi                                     *Manager
		db                                             *gorm.DB
		id                                             strfmt.UUID
		cluster                                        common.Cluster
		dbName                                         string
		ctrl                                           *gomock.Controller
		mockEvents                                     *eventsapi.MockHandler
		apiV4Vip, apiV6Vip, ingressV4Vip, ingressV6Vip string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		apiV4Vip = "1.2.3.5"
		ingressV4Vip = "1.2.3.6"
		apiV6Vip = "1001:db8::100"
		ingressV6Vip = "1001:db8::101"
	})

	createPayload := func(response models.VerifyVipsResponse) string {
		b, err := json.Marshal(&response)
		Expect(err)
		return string(b)
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
	createClusterAndHosts := func(apivVips, ingressVips []string) {
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(models.ClusterStatusReady),
			ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips:         funk.Map(apivVips, func(apiVip string) *models.APIVip { return &models.APIVip{IP: models.IP(apiVip), ClusterID: id} }).([]*models.APIVip),
			IngressVips: funk.Map(ingressVips,
				func(ingressVip string) *models.IngressVip {
					return &models.IngressVip{IP: models.IP(ingressVip), ClusterID: id}
				}).([]*models.IngressVip),
			BaseDNSDomain: "test.com",
			PullSecretSet: true,
			NetworkType:   swag.String(models.ClusterNetworkTypeOVNKubernetes),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	}
	expect := func(response models.VerifyVipsResponse, updateExpected bool, timestamp time.Time) {
		cls, err := common.GetClusterFromDB(db, id, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).To(HaveLen(len(cls.APIVips) + len(cls.IngressVips)))
		Expect(funk.Map(funk.Filter(response, func(r *models.VerifiedVip) bool { return r.VipType == models.VipTypeAPI }),
			func(v *models.VerifiedVip) *models.APIVip {
				return &models.APIVip{IP: v.Vip, ClusterID: id, Verification: v.Verification}
			})).To(ConsistOf(cls.APIVips))
		Expect(funk.Map(funk.Filter(response, func(r *models.VerifiedVip) bool { return r.VipType == models.VipTypeIngress }),
			func(v *models.VerifiedVip) *models.IngressVip {
				return &models.IngressVip{IP: v.Vip, ClusterID: id, Verification: v.Verification}
			})).To(ConsistOf(cls.IngressVips))
		if updateExpected {
			Expect(cls.TriggerMonitorTimestamp.After(timestamp)).To(BeTrue())
		}
	}
	It("happy flow", func() {
		createClusterAndHosts([]string{apiV4Vip, apiV6Vip}, []string{ingressV4Vip, ingressV6Vip})
		response := models.VerifyVipsResponse{
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV4Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV6Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV4Vip),
				VipType:      models.VipTypeIngress,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV6Vip),
				VipType:      models.VipTypeIngress,
			},
		}
		payload := createPayload(response)
		timestamp := time.Now()
		err := clusterApi.HandleVerifyVipsResponse(ctx, id, payload)
		Expect(err).ToNot(HaveOccurred())
		expect(response, true, timestamp)
	})
	It("no vips exist", func() {
		createClusterAndHosts([]string{}, []string{})
		response := models.VerifyVipsResponse{
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV4Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV6Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV4Vip),
				VipType:      models.VipTypeIngress,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV6Vip),
				VipType:      models.VipTypeIngress,
			},
		}
		payload := createPayload(response)
		timestamp := time.Now()
		err := clusterApi.HandleVerifyVipsResponse(ctx, id, payload)
		Expect(err).ToNot(HaveOccurred())
		expect(models.VerifyVipsResponse{}, false, timestamp)
	})
	It("already verified", func() {
		createClusterAndHosts([]string{apiV4Vip, apiV6Vip}, []string{ingressV4Vip, ingressV6Vip})
		Expect(db.Model(&models.APIVip{}).Where("cluster_id = ?", id.String()).Update("verification", models.VipVerificationSucceeded).Error).ToNot(HaveOccurred())
		Expect(db.Model(&models.IngressVip{}).Where("cluster_id = ?", id.String()).Update("verification", models.VipVerificationSucceeded).Error).ToNot(HaveOccurred())
		response := models.VerifyVipsResponse{
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV4Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV6Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV4Vip),
				VipType:      models.VipTypeIngress,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV6Vip),
				VipType:      models.VipTypeIngress,
			},
		}
		payload := createPayload(response)
		timestamp := time.Now()
		err := clusterApi.HandleVerifyVipsResponse(ctx, id, payload)
		Expect(err).ToNot(HaveOccurred())
		expect(response, false, timestamp)
	})
	It(" move 1 to failed", func() {
		createClusterAndHosts([]string{apiV4Vip, apiV6Vip}, []string{ingressV4Vip, ingressV6Vip})
		Expect(db.Model(&models.APIVip{}).Where("cluster_id = ?", id.String()).Update("verification", models.VipVerificationSucceeded).Error).ToNot(HaveOccurred())
		Expect(db.Model(&models.IngressVip{}).Where("cluster_id = ?", id.String()).Update("verification", models.VipVerificationSucceeded).Error).ToNot(HaveOccurred())
		response := models.VerifyVipsResponse{
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV4Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(apiV6Vip),
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationFailed),
				Vip:          models.IP(ingressV4Vip),
				VipType:      models.VipTypeIngress,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          models.IP(ingressV6Vip),
				VipType:      models.VipTypeIngress,
			},
		}
		payload := createPayload(response)
		timestamp := time.Now()
		err := clusterApi.HandleVerifyVipsResponse(ctx, id, payload)
		Expect(err).ToNot(HaveOccurred())
		expect(response, true, timestamp)
	})
})

var _ = Describe("ready_state", func() {
	var (
		ctx        = context.Background()
		clusterApi *Manager
		db         *gorm.DB
		id         strfmt.UUID
		cluster    common.Cluster
		dbName     string
		ctrl       *gomock.Controller
		mockEvents *eventsapi.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(models.ClusterStatusReady),
			ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips:         []*models.APIVip{{IP: models.IP(apiVip), ClusterID: id, Verification: common.VipVerificationPtr(models.VipVerificationSucceeded)}},
			IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: id, Verification: common.VipVerificationPtr(models.VipVerificationSucceeded)}},
			BaseDNSDomain:   "test.com",
			PullSecretSet:   true,
			NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		addInstallationRequirements(id, db)

		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusReady))
		Expect(len(cluster.Hosts)).Should(Equal(3))
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
			eventstest.WithClusterIdMatcher(cluster.ID.String()))).AnyTimes()

		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("refresh_state", func() {
		It("cluster is satisfying the install requirements", func() {
			clusterAfterRefresh, updateErr := clusterApi.RefreshStatus(ctx, &cluster, db)
			Expect(updateErr).Should(BeNil())

			preSecondRefreshUpdatedTime := clusterAfterRefresh.UpdatedAt
			clusterAfterRefresh, updateErr = clusterApi.RefreshStatus(ctx, clusterAfterRefresh, db)
			postRefreshUpdateTime := clusterAfterRefresh.UpdatedAt

			Expect(preSecondRefreshUpdatedTime).Should(Equal(postRefreshUpdateTime))

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(models.ClusterStatusReady))
			Expect(checkValidationInfoIsSorted(clusterAfterRefresh.ValidationsInfo)).Should(BeTrue())
		})

		It("cluster is not satisfying the install requirements", func() {
			Expect(db.Where("cluster_id = ?", cluster.ID).Delete(&models.Host{}).Error).NotTo(HaveOccurred())

			cluster = getClusterFromDB(*cluster.ID, db)
			clusterAfterRefresh, updateErr := clusterApi.RefreshStatus(ctx, &cluster, db)

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(models.ClusterStatusInsufficient))
			Expect(checkValidationInfoIsSorted(clusterAfterRefresh.ValidationsInfo)).Should(BeTrue())
		})
	})
})

func checkValidationInfoIsSorted(validationInfo string) bool {
	validationsOutput := make(map[string][]ValidationResult)
	Expect(json.Unmarshal([]byte(validationInfo), &validationsOutput)).ToNot(HaveOccurred())

	for _, v := range validationsOutput {
		vRes := v
		sortedByID := sort.SliceIsSorted(vRes, func(i, j int) bool {
			return vRes[i].ID < vRes[j].ID
		})
		if !sortedByID {
			return false
		}
	}
	return true
}

var _ = Describe("insufficient_state", func() {
	var (
		ctx          = context.Background()
		clusterApi   *Manager
		db           *gorm.DB
		currentState = models.ClusterStatusInsufficient
		id           strfmt.UUID
		cluster      common.Cluster
		ctrl         *gomock.Controller
		mockHostAPI  *host.MockAPI
		dbName       string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := eventsapi.NewMockHandler(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(currentState),
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips:         common.TestIPv4Networking.APIVips,
			IngressVips:     common.TestIPv4Networking.IngressVips,
			BaseDNSDomain:   "test.com",
			PullSecretSet:   true,
		}}
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName),
			eventstest.WithClusterIdMatcher(id.String()))).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("works", func() {
		replyErr := clusterApi.RegisterCluster(ctx, &cluster)
		Expect(replyErr).Should(BeNil())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		c := getClusterFromDB(*cluster.ID, db)
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
	})
})

var _ = Describe("prepare-for-installation refresh status", func() {
	var (
		ctx           = context.Background()
		capi          API
		db            *gorm.DB
		clusterId     strfmt.UUID
		cl            common.Cluster
		dbName        string
		ctrl          *gomock.Controller
		mockHostAPI   *host.MockAPI
		mockOperators *operators.MockAPI
		manifestsAPI  *manifestsapi.MockManifestsAPI
		mockEvents    *eventsapi.MockHandler
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).NotTo(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockOperators = operators.NewMockAPI(ctrl)
		manifestsAPI = manifestsapi.NewMockManifestsAPI(ctrl)
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		dummy := &leader.DummyElector{}
		capi = NewManager(cfg, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil, manifestsAPI)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("no change", func() {
		Expect(db.Take(&cl, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(*refreshedCluster.Status).To(Equal(models.ClusterStatusPreparingForInstallation))
	})

	It("timeout - assisted pod failure", func() {
		// In the case of assisted pod failure, all of the hosts are likely to be successful
		// This is detecting the case where assisted pod failure is the only reason that the cluster failed to prepare.
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		h1ID := strfmt.UUID(uuid.New().String())
		h1 := common.Host{
			Host: models.Host{
				ID:              &h1ID,
				Status:          swag.String(models.HostStatusPreparingSuccessful),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
				ClusterID:       &clusterId,
			},
		}
		h2ID := strfmt.UUID(uuid.New().String())
		h2 := common.Host{
			Host: models.Host{
				ID:              &h2ID,
				Status:          swag.String(models.HostStatusPreparingSuccessful),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
				ClusterID:       &clusterId,
			},
		}
		h3ID := strfmt.UUID(uuid.New().String())
		h3 := common.Host{
			Host: models.Host{
				ID:              &h3ID,
				Status:          swag.String(models.HostStatusPreparingSuccessful),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
				ClusterID:       &clusterId,
			},
		}
		Expect(db.Create(&h1).Error).NotTo(HaveOccurred())
		Expect(db.Create(&h2).Error).NotTo(HaveOccurred())
		Expect(db.Create(&h3).Error).NotTo(HaveOccurred())

		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()),
			eventstest.WithMessageContainsMatcher("Installation failed. Try again, and contact support if the issue persists."))).Times(1)
		Expect(db.Model(&cl).Update("status_updated_at", strfmt.DateTime(time.Now().Add(-15*time.Minute))).Error).
			NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(refreshedCluster.Status)).To(Equal(models.ClusterStatusReady))
	})
})

var _ = Describe("Cluster tarred files", func() {
	var (
		ctx                   = context.Background()
		capi                  API
		db                    *gorm.DB
		clusterId             strfmt.UUID
		cl                    common.Cluster
		dbName                string
		ctrl                  *gomock.Controller
		mockHostAPI           *host.MockAPI
		mockEvents            *eventsapi.MockHandler
		mockS3Client          *s3wrapper.MockAPI
		prefix                string
		files                 []string
		tarFile               string
		clusterObjectFilename string
		eventsFilename        string
		mockManifestApi       *manifestsapi.MockManifestsAPI
	)

	uploadClusterDataSuccess := func() {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), clusterObjectFilename).Return(nil).Times(1)

		events := []*common.Event{{
			Event: models.Event{
				Name: "test",
			},
		}}
		mockEvents.EXPECT().V2GetEvents(gomock.Any(), common.GetDefaultV2GetEventsParams(cl.ID, nil, nil)).Return(&common.V2GetEventsResponse{Events: events}, nil).Times(1)
		eventsData, _ := json.MarshalIndent(events, "", " ")
		mockS3Client.EXPECT().Upload(ctx, eventsData, eventsFilename).Return(nil).Times(1)
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cfg := Config{}
		files = []string{"test", "test2"}
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		mockManifestApi = manifestsapi.NewMockManifestsAPI(ctrl)
		capi = NewManager(cfg, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil, mockManifestApi)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		clusterObjectFilename = fmt.Sprintf("%s/logs/cluster/metadata.json", cl.ID)
		eventsFilename = fmt.Sprintf("%s/logs/cluster/events.json", cl.ID)
		tarFile = fmt.Sprintf("%s/logs/cluster_logs.tar", clusterId)
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
		prefix = fmt.Sprintf("%s/logs/", cl.ID)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
		mockManifestApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("list events failed - but PrepareClusterLogFile should continue to download", func() {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), clusterObjectFilename).Return(nil).Times(1)
		mockEvents.EXPECT().V2GetEvents(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("dummy")).Times(1)
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("upload events failed - but PrepareClusterLogFile should continue to download", func() {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), clusterObjectFilename).Return(nil).Times(1)
		mockEvents.EXPECT().V2GetEvents(gomock.Any(), common.GetDefaultV2GetEventsParams(cl.ID, nil, nil)).Return(&common.V2GetEventsResponse{}, nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("dummy")).Times(1)

		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("upload cluster data failed - but PrepareClusterLogFile should continue to download", func() {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("dummy")).Times(1)
		mockEvents.EXPECT().V2GetEvents(gomock.Any(), common.GetDefaultV2GetEventsParams(cl.ID, nil, nil)).Return(&common.V2GetEventsResponse{}, nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)

		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects failed", func() {
		uploadClusterDataSuccess()
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(nil, errors.Errorf("dummy"))
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects no files", func() {
		uploadClusterDataSuccess()
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{tarFile}, nil)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects only all logs file", func() {
		uploadClusterDataSuccess()
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{}, nil)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("download failed", func() {
		uploadClusterDataSuccess()
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("upload failed", func() {
		uploadClusterDataSuccess()
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, gomock.Any()).Return(r, int64(4), nil).AnyTimes()
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), tarFile).Return(errors.Errorf("Dummy")).Times(1)
		_, err := capi.PrepareClusterLogFile(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("GenerateAdditionalManifests", func() {
	var (
		ctrl               *gomock.Controller
		ctx                = context.Background()
		db                 *gorm.DB
		capi               API
		c                  common.Cluster
		eventsHandler      eventsapi.Handler
		mockMetric         *metrics.MockAPI
		dbName             string
		manifestsGenerator *network.MockManifestsGeneratorAPI
		mockOperatorMgr    *operators.MockAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		manifestsGenerator = network.NewMockManifestsGeneratorAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		dummy := &leader.DummyElector{}
		mockOperatorMgr = operators.NewMockAPI(ctrl)
		cfg := getDefaultConfig()
		capi = NewManager(cfg, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, manifestsGenerator, dummy, mockOperatorMgr, nil, nil, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusReady),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Add manifest failure", func() {
		manifestsGenerator.EXPECT().AddChronyManifest(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error")).Times(1)
		err := capi.GenerateAdditionalManifests(ctx, &c)
		Expect(err).To(HaveOccurred())
	})

	It("Single node manifests success", func() {
		manifestsGenerator.EXPECT().AddChronyManifest(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().IsSNODNSMasqEnabled().Return(true).Times(1)
		manifestsGenerator.EXPECT().AddDnsmasqForSingleNode(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().AddNodeIpHint(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), &c).Return(nil)
		manifestsGenerator.EXPECT().AddDiskEncryptionManifest(ctx, gomock.Any(), &c).Return(nil)
		mockOperatorMgr.EXPECT().GenerateManifests(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		c.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		err := capi.GenerateAdditionalManifests(ctx, &c)
		Expect(err).To(Not(HaveOccurred()))
	})

	It("Single node manifests failure", func() {
		manifestsGenerator.EXPECT().AddChronyManifest(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().IsSNODNSMasqEnabled().Return(true).Times(1)
		manifestsGenerator.EXPECT().AddDnsmasqForSingleNode(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error")).Times(1)
		c.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		err := capi.GenerateAdditionalManifests(ctx, &c)
		Expect(err).To(HaveOccurred())
	})

	It("Single node manifests success with disabled dnsmasq", func() {
		cfg2 := getDefaultConfig()
		capi = NewManager(cfg2, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, manifestsGenerator, nil, mockOperatorMgr, nil, nil, nil, nil, nil)
		manifestsGenerator.EXPECT().AddChronyManifest(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().IsSNODNSMasqEnabled().Return(false).Times(1)
		manifestsGenerator.EXPECT().AddNodeIpHint(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), &c).Return(nil)
		manifestsGenerator.EXPECT().AddDiskEncryptionManifest(ctx, gomock.Any(), &c).Return(nil)
		mockOperatorMgr.EXPECT().GenerateManifests(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		c.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		err := capi.GenerateAdditionalManifests(ctx, &c)
		Expect(err).To(Not(HaveOccurred()))
	})

	Context("Telemeter", func() {

		var (
			telemeterCfg Config
			capi         API
		)

		BeforeEach(func() {
			telemeterCfg = getDefaultConfig()
			capi = NewManager(telemeterCfg, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, manifestsGenerator, nil, mockOperatorMgr, nil, nil, nil, nil, nil)
		})

		It("Happy flow", func() {

			manifestsGenerator.EXPECT().AddChronyManifest(ctx, gomock.Any(), &c).Return(nil)
			mockOperatorMgr.EXPECT().GenerateManifests(ctx, &c).Return(nil)
			manifestsGenerator.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), &c).Return(nil)
			manifestsGenerator.EXPECT().AddNodeIpHint(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsGenerator.EXPECT().AddDiskEncryptionManifest(ctx, gomock.Any(), &c).Return(nil)

			err := capi.GenerateAdditionalManifests(ctx, &c)
			Expect(err).To(Not(HaveOccurred()))
		})

		It("AddTelemeterManifest failed", func() {

			manifestsGenerator.EXPECT().AddChronyManifest(ctx, gomock.Any(), &c).Return(nil)
			mockOperatorMgr.EXPECT().GenerateManifests(ctx, &c).Return(nil)
			manifestsGenerator.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), &c).Return(errors.New("dummy"))

			err := capi.GenerateAdditionalManifests(ctx, &c)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Deregister inactive clusters", func() {
	var (
		ctrl          *gomock.Controller
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler eventsapi.Handler
		mockMetric    *metrics.MockAPI
		dbName        string
		mockS3Client  *s3wrapper.MockAPI
	)

	registerCluster := func() common.Cluster {
		id := strfmt.UUID(uuid.New().String())
		eventgen.SendInactiveClustersDeregisteredEvent(ctx, eventsHandler, id, "")
		cl := common.Cluster{Cluster: models.Cluster{
			ID:                 &id,
			MonitoredOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator},
		}}
		Expect(db.Create(&cl).Error).ShouldNot(HaveOccurred())
		return cl
	}

	wasDeregisterd := func(db *gorm.DB, clusterId strfmt.UUID) bool {
		c, err := common.GetClusterFromDBWhere(db, common.UseEagerLoading, true, "id = ?", clusterId.String())
		Expect(err).ShouldNot(HaveOccurred())
		return c.DeletedAt.Valid
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		dummy := &leader.DummyElector{}
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, nil, dummy, mockOperators, nil, mockS3Client, nil, nil, nil)
		c = registerCluster()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Deregister inactive cluster", func() {
		Expect(state.DeregisterInactiveCluster(ctx, 10, strfmt.DateTime(time.Now()))).ShouldNot(HaveOccurred())
		Expect(wasDeregisterd(db, *c.ID)).To(BeTrue())
	})

	It("Do noting, active cluster", func() {
		lastActive := strfmt.DateTime(time.Now().Add(-time.Hour))
		Expect(state.DeregisterInactiveCluster(ctx, 10, lastActive)).ShouldNot(HaveOccurred())
		Expect(wasDeregisterd(db, *c.ID)).To(BeFalse())
	})

	It("Deregister inactive cluster with new clusters", func() {
		inactiveCluster1 := registerCluster()
		inactiveCluster2 := registerCluster()
		inactiveCluster3 := registerCluster()

		// To verify that lastActive is greater than the updatedAt field of inactiveCluster3
		time.Sleep(time.Millisecond)
		lastActive := strfmt.DateTime(time.Now())

		activeCluster1 := registerCluster()
		activeCluster2 := registerCluster()
		activeCluster3 := registerCluster()

		Expect(state.DeregisterInactiveCluster(ctx, 10, lastActive)).ShouldNot(HaveOccurred())

		Expect(wasDeregisterd(db, *inactiveCluster1.ID)).To(BeTrue())
		Expect(wasDeregisterd(db, *inactiveCluster2.ID)).To(BeTrue())
		Expect(wasDeregisterd(db, *inactiveCluster3.ID)).To(BeTrue())

		Expect(wasDeregisterd(db, *activeCluster1.ID)).To(BeFalse())
		Expect(wasDeregisterd(db, *activeCluster2.ID)).To(BeFalse())
		Expect(wasDeregisterd(db, *activeCluster3.ID)).To(BeFalse())
	})

	It("Deregister inactive cluster limited", func() {
		var (
			clustersBeforeDeregisterInactive []*common.Cluster
			clustersAfterDeregisterInactive  []*common.Cluster
		)

		for i := 0; i < 6; i++ {
			registerCluster()
		}

		lastActivePlus5sec := strfmt.DateTime(time.Now().Add(5 * time.Second))

		err := db.Find(&clustersBeforeDeregisterInactive).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(clustersBeforeDeregisterInactive).To(HaveLen(7))

		Expect(state.DeregisterInactiveCluster(ctx, 3, lastActivePlus5sec)).ShouldNot(HaveOccurred())

		err = db.Find(&clustersAfterDeregisterInactive).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(clustersAfterDeregisterInactive).To(HaveLen(4))
	})
})

var _ = Describe("Permanently delete clusters", func() {

	var (
		ctrl          *gomock.Controller
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c1            common.Cluster
		c2            common.Cluster
		c3            common.Cluster
		eventsHandler eventsapi.Handler
		mockMetric    *metrics.MockAPI
		dbName        string
		mockS3Api     *s3wrapper.MockAPI
		manifestsAPI  *manifestsapi.MockManifestsAPI
	)

	registerCluster := func() common.Cluster {
		id := strfmt.UUID(uuid.New().String())
		eventgen.SendClustersPermanentlyDeletedEvent(ctx, eventsHandler, id, "")
		c := common.Cluster{Cluster: models.Cluster{
			ID:                 &id,
			MonitoredOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator},
			ClusterNetworks:    common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks:    common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks:    common.TestIPv4Networking.MachineNetworks,
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())

		c = getClusterFromDB(*c.ID, db)
		Expect(c.MonitoredOperators).ToNot(BeEmpty())
		Expect(c.ClusterNetworks).ToNot(BeEmpty())
		Expect(c.ServiceNetworks).ToNot(BeEmpty())
		Expect(c.MachineNetworks).ToNot(BeEmpty())

		return c
	}

	verifyClusterSubComponentsDeletion := func(clusterID strfmt.UUID, isDeleted bool) {
		ExpectWithOffset(1, db.Unscoped().Where("id = ?", clusterID).Find(&common.Cluster{}).RowsAffected == 0).Should(Equal(isDeleted))

		response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(&clusterID, nil, nil))
		Expect(err).NotTo(HaveOccurred())
		clusterEvents := response.GetEvents()
		Expect(len(clusterEvents) == 0).Should(Equal(isDeleted))

		var operators []*models.MonitoredOperator
		Expect(db.Unscoped().Find(&operators, "cluster_id = ?", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(len(operators) == 0).Should(Equal(isDeleted))

		var clusterNetworks []*models.ClusterNetwork
		Expect(db.Unscoped().Find(&clusterNetworks, "cluster_id = ?", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(len(clusterNetworks) == 0).Should(Equal(isDeleted))

		var serviceNetworks []*models.ServiceNetwork
		Expect(db.Unscoped().Find(&serviceNetworks, "cluster_id = ?", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(len(serviceNetworks) == 0).Should(Equal(isDeleted))

		var machineNetworks []*models.MachineNetwork
		Expect(db.Unscoped().Find(&machineNetworks, "cluster_id = ?", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(len(machineNetworks) == 0).Should(Equal(isDeleted))
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		manifestsAPI = manifestsapi.NewMockManifestsAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil, manifestsAPI)
		c1 = registerCluster()
		c2 = registerCluster()
		c3 = registerCluster()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("permanently delete clusters success", func() {
		Expect(db.Delete(&c1).RowsAffected).Should(Equal(int64(1)))
		Expect(db.First(&common.Cluster{}, "id = ?", c1.ID).RowsAffected).Should(Equal(int64(0)))
		Expect(db.Unscoped().First(&common.Cluster{}, "id = ?", c1.ID).RowsAffected).Should(Equal(int64(1)))

		Expect(db.Delete(&c2).RowsAffected).Should(Equal(int64(1)))
		Expect(db.First(&common.Cluster{}, "id = ?", c2.ID).RowsAffected).Should(Equal(int64(0)))
		Expect(db.Unscoped().First(&common.Cluster{}, "id = ?", c2.ID).RowsAffected).Should(Equal(int64(1)))

		mockS3Api.EXPECT().DeleteObject(gomock.Any(), c1.ID.String()).Return(false, nil).Times(1)
		mockS3Api.EXPECT().DeleteObject(gomock.Any(), c2.ID.String()).Return(false, nil).Times(1)
		mockS3Api.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return([]string{}, nil).AnyTimes()

		Expect(state.PermanentClustersDeletion(ctx, strfmt.DateTime(time.Now().Add(time.Minute)), mockS3Api)).ShouldNot(HaveOccurred())

		verifyClusterSubComponentsDeletion(*c1.ID, true)

		verifyClusterSubComponentsDeletion(*c2.ID, true)
		verifyClusterSubComponentsDeletion(*c3.ID, false)
	})

	It("permanently delete clusters - nothing to delete", func() {
		deletedAt := strfmt.DateTime(time.Now().Add(-time.Hour))
		Expect(state.PermanentClustersDeletion(ctx, deletedAt, mockS3Api)).ShouldNot(HaveOccurred())

		verifyClusterSubComponentsDeletion(*c1.ID, false)
		verifyClusterSubComponentsDeletion(*c2.ID, false)
		verifyClusterSubComponentsDeletion(*c3.ID, false)
	})
})

var _ = Describe("Get cluster by Kube key", func() {
	var (
		state            API
		ctrl             *gomock.Controller
		db               *gorm.DB
		eventsHandler    eventsapi.Handler
		key              types.NamespacedName
		dbName           string
		kubeKeyName      = "test-kube-name"
		kubeKeyNamespace = "test-kube-namespace"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, nil, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		key = types.NamespacedName{
			Namespace: kubeKeyNamespace,
			Name:      kubeKeyName,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("cluster not exist", func() {
		c, err := state.GetClusterByKubeKey(key)
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
		Expect(c).Should(BeNil())
	})

	It("get cluster by kube key success", func() {
		id := strfmt.UUID(uuid.New().String())
		c1 := common.Cluster{
			KubeKeyName:      kubeKeyName,
			KubeKeyNamespace: kubeKeyNamespace,
			Cluster:          models.Cluster{ID: &id},
		}
		Expect(db.Create(&c1).Error).ShouldNot(HaveOccurred())

		c2, err := state.GetClusterByKubeKey(key)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(c2.ID.String()).Should(Equal(c1.ID.String()))
	})
})

var _ = Describe("Transform day1 cluster to a day2 cluster", func() {

	var (
		db         *gorm.DB
		clusterApi *Manager
		ctrl       *gomock.Controller
		cfg        Config
		ctx        = context.Background()
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name               string
		clusterStatus      string
		clusterKind        string
		errorExpected      bool
		userManagedNetwork bool
		apiVips            []*models.APIVip
	}{
		{
			name:               "successfully transform day1 cluster to a day2 cluster - status installed - user managed network true",
			clusterStatus:      models.ClusterStatusInstalled,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      false,
			userManagedNetwork: true,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "successfully transform day1 cluster to a day2 cluster - status installed - api vip is not set",
			clusterStatus:      models.ClusterStatusInstalled,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      false,
			userManagedNetwork: false,
			apiVips:            nil,
		},
		{
			name:               "successfully transform day1 cluster to a day2 cluster - status installed",
			clusterStatus:      models.ClusterStatusInstalled,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      false,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - kind AddHostsCluster",
			clusterStatus:      models.ClusterStatusInstalled,
			clusterKind:        models.ClusterKindAddHostsCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status insufficient",
			clusterStatus:      models.ClusterStatusInsufficient,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status ready",
			clusterStatus:      models.ClusterStatusReady,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status error",
			clusterStatus:      models.ClusterStatusError,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - preparing-for-installation",
			clusterStatus:      models.ClusterStatusPreparingForInstallation,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status pending-for-input",
			clusterStatus:      models.ClusterStatusPendingForInput,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status installing",
			clusterStatus:      models.ClusterStatusInstalling,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status finalizing",
			clusterStatus:      models.ClusterStatusFinalizing,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status adding-hosts",
			clusterStatus:      models.ClusterStatusAddingHosts,
			clusterKind:        models.ClusterKindAddHostsCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status cancelled",
			clusterStatus:      models.ClusterStatusCancelled,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
		{
			name:               "fail to transform day1 cluster to a day2 cluster - status installing-pending-user-action",
			clusterStatus:      models.ClusterStatusInstallingPendingUserAction,
			clusterKind:        models.ClusterKindCluster,
			errorExpected:      true,
			userManagedNetwork: false,
			apiVips:            common.TestIPv4Networking.APIVips,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			id := strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{
				ID:                    &id,
				Kind:                  swag.String(t.clusterKind),
				OpenshiftVersion:      common.TestDefaultConfig.OpenShiftVersion,
				Status:                swag.String(t.clusterStatus),
				MachineNetworks:       common.TestIPv4Networking.MachineNetworks,
				APIVips:               t.apiVips,
				IngressVips:           common.TestIPv4Networking.IngressVips,
				BaseDNSDomain:         "test.com",
				PullSecretSet:         true,
				UserManagedNetworking: swag.Bool(t.userManagedNetwork),
			}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			err1 := clusterApi.TransformClusterToDay2(ctx, cluster, db)
			Expect(err1 != nil).To(Equal(t.errorExpected))
			if !t.errorExpected {
				var c common.Cluster
				Expect(db.Take(&c, "id = ?", cluster.ID.String()).Error).ToNot(HaveOccurred())
				Expect(c.Kind).To(Equal(swag.String(models.ClusterKindAddHostsCluster)))
				Expect(c.Status).To(Equal(swag.String(models.ClusterStatusAddingHosts)))
				if t.apiVips == nil || t.userManagedNetwork {
					apiVipDnsname := fmt.Sprintf("api.%s.%s", c.Name, c.BaseDNSDomain)
					Expect(c.APIVipDNSName).To(Equal(swag.String(apiVipDnsname)))
				} else {
					Expect(c.APIVipDNSName).To(Equal(swag.String(network.GetApiVipById(cluster, 0))))
				}

			}

		})
	}
})

var _ = Describe("Update AMS subscription ID", func() {

	var (
		ctrl          *gomock.Controller
		ctx           = context.Background()
		db            *gorm.DB
		dbName        string
		eventsHandler eventsapi.Handler
		api           API
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.New())
		api = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), eventsHandler, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Update AMS subscription ID", func() {

		clusterID := strfmt.UUID(uuid.New().String())
		c := common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
			},
		}
		err := api.RegisterCluster(ctx, &c)
		Expect(err).ShouldNot(HaveOccurred())

		subID := strfmt.UUID(uuid.New().String())
		err = api.UpdateAmsSubscriptionID(ctx, clusterID, subID)
		Expect(err).ShouldNot(HaveOccurred())

		c = getClusterFromDB(*c.ID, db)
		Expect(c.AmsSubscriptionID).To(Equal(subID))
	})
})

var _ = Describe("Validation metrics and events", func() {

	const (
		openshiftVersion = "dummyVersion"
		emailDomain      = "dummy.test"
	)

	var (
		ctrl         *gomock.Controller
		ctx          = context.Background()
		db           *gorm.DB
		dbName       string
		mockEvents   *eventsapi.MockHandler
		mockHost     *host.MockAPI
		mockMetric   *metrics.MockAPI
		m            *Manager
		c            *common.Cluster
		mockS3Client *s3wrapper.MockAPI
	)

	generateTestValidationResult := func(status ValidationStatus) ValidationsStatus {
		validationRes := ValidationsStatus{
			"hw": {
				{
					ID:     SufficientMastersCount,
					Status: status,
				},
			},
		}
		return validationRes
	}

	registerTestClusterWithValidationsAndHost := func() *common.Cluster {

		clusterID := strfmt.UUID(uuid.New().String())
		c := common.Cluster{
			Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: openshiftVersion,
				EmailDomain:      emailDomain,
			},
		}
		validationRes := generateTestValidationResult(ValidationFailure)
		bytes, err := json.Marshal(validationRes)
		Expect(err).ShouldNot(HaveOccurred())
		c.ValidationsInfo = string(bytes)
		err = m.RegisterCluster(ctx, &c)
		Expect(err).ShouldNot(HaveOccurred())

		createHost(clusterID, models.HostStatusInsufficient, db)

		c = getClusterFromDB(clusterID, db)
		return &c
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHost = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		m = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, mockHost, mockMetric, nil, nil, nil, nil, mockS3Client, nil, nil, nil)
		c = registerTestClusterWithValidationsAndHost()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Test DeregisterCluster", func() {
		mockHost.EXPECT().ReportValidationFailedMetrics(ctx, gomock.Any(), openshiftVersion, emailDomain)
		mockMetric.EXPECT().ClusterValidationFailed(models.ClusterValidationIDSufficientMastersCount)
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterDeregisteredEventName),
			eventstest.WithClusterIdMatcher(c.ID.String())))
		err := m.DeregisterCluster(ctx, c)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Test reportValidationStatusChanged", func() {

		// Pending -> Success
		mockEvents.EXPECT().NotifyInternalEvent(ctx, c.ID, nil, nil, gomock.Any())
		currentValidationRes := generateTestValidationResult(ValidationPending)
		newValidationRes := generateTestValidationResult(ValidationSuccess)
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)

		// Pending -> Failure
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterValidationFailedEventName),
			eventstest.WithClusterIdMatcher(c.ID.String())))
		currentValidationRes = generateTestValidationResult(ValidationPending)
		newValidationRes = generateTestValidationResult(ValidationFailure)
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)

		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(c.ID.String())))
		newValidationRes = generateTestValidationResult(ValidationSuccess)
		err := json.Unmarshal([]byte(c.ValidationsInfo), &currentValidationRes)
		Expect(err).ToNot(HaveOccurred())
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)

		mockMetric.EXPECT().ClusterValidationChanged(models.ClusterValidationIDSufficientMastersCount)
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(c.ID.String())))

		currentValidationRes = newValidationRes
		newValidationRes = generateTestValidationResult(ValidationFailure)
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)
	})
})

var _ = Describe("Console-operator's availability", func() {

	var (
		ctrl       *gomock.Controller
		db         *gorm.DB
		dbName     string
		clusterApi API
		mockEvents *eventsapi.MockHandler
		c          common.Cluster
	)

	createCluster := func(status string, operators []*models.MonitoredOperator) common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			Status:             swag.String(status),
			MonitoredOperators: operators,
		}}
		Expect(common.LoadTableFromDB(db, common.MonitoredOperatorsTable).Create(&cluster).Error).ShouldNot(HaveOccurred())

		return cluster
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name          string
		ops           []*models.MonitoredOperator
		clusterStatus string
		result        bool
	}{
		{
			name: "Operator available",
			ops: []*models.MonitoredOperator{
				{
					Name:         operators.OperatorConsole.Name,
					OperatorType: models.OperatorTypeBuiltin,
					Status:       models.OperatorStatusAvailable,
				},
			},
			clusterStatus: models.ClusterStatusFinalizing,
			result:        true,
		},
		{
			name: "Operator can't be found",
			ops: []*models.MonitoredOperator{
				{
					Name:         operators.OperatorCVO.Name,
					OperatorType: models.OperatorTypeBuiltin,
					Status:       models.OperatorStatusAvailable,
				},
			},
			clusterStatus: models.ClusterStatusFinalizing,
			result:        false,
		},
		{
			name: "Operator progressing",
			ops: []*models.MonitoredOperator{
				{
					Name:         operators.OperatorConsole.Name,
					OperatorType: models.OperatorTypeBuiltin,
					Status:       models.OperatorStatusProgressing,
				},
			},
			clusterStatus: models.ClusterStatusFinalizing,
			result:        false,
		},
		{
			name: "Operator failed",
			ops: []*models.MonitoredOperator{
				{
					Name:         operators.OperatorConsole.Name,
					OperatorType: models.OperatorTypeBuiltin,
					Status:       models.OperatorStatusFailed,
				},
			},
			clusterStatus: models.ClusterStatusFinalizing,
			result:        false,
		},
		{
			// TODO: MGMT-4458
			// Backward-compatible solution for clusters that don't have monitored operators data
			name:          "No operators (Backward-compatible) - finalizing",
			ops:           []*models.MonitoredOperator{},
			clusterStatus: models.ClusterStatusFinalizing,
			result:        true,
		},
		{
			// TODO: MGMT-4458
			// Backward-compatible solution for clusters that don't have monitored operators data
			name:          "No operators (Backward-compatible) - insufficient",
			ops:           []*models.MonitoredOperator{},
			clusterStatus: models.ClusterStatusInsufficient,
			result:        false,
		},
	}

	for i := range tests {
		test := tests[i]
		It(test.name, func() {
			c = createCluster(test.clusterStatus, test.ops)
			Expect(clusterApi.IsOperatorAvailable(&c, operators.OperatorConsole.Name)).To(Equal(test.result))
		})
	}
})

var _ = Describe("Test RefreshSchedulableMastersForcedTrue", func() {

	var (
		ctrl       *gomock.Controller
		ctx        = context.Background()
		db         *gorm.DB
		dbName     string
		clusterApi API
	)

	createCluster := func(schedulableMastersForcedTrue *bool) common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:                           &clusterID,
				Status:                       swag.String(models.ClusterStatusReady),
				SchedulableMastersForcedTrue: schedulableMastersForcedTrue,
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		return cluster
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("schedulableMastersForcedTrue should be set to false when MinHostsDisableSchedulableMasters hosts or more are registered with the cluster", func() {
		cluster := createCluster(swag.Bool(true))
		for hostCount := 0; hostCount < ForceSchedulableMastersMaxHostCount; hostCount++ {
			createHost(*cluster.ID, "", db)
		}

		err := clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *cluster.ID)
		Expect(err).ToNot(HaveOccurred())

		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.BoolValue(cluster.SchedulableMastersForcedTrue)).To(Equal(false))
	})

	It("schedulableMastersForcedTrue should be set to false less then MinHostsDisableSchedulableMasters hosts are registered with the cluster", func() {
		cluster := createCluster(swag.Bool(false))
		for hostCount := 0; hostCount < ForceSchedulableMastersMaxHostCount-1; hostCount++ {
			createHost(*cluster.ID, "", db)
		}

		err := clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *cluster.ID)
		Expect(err).ToNot(HaveOccurred())

		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.BoolValue(cluster.SchedulableMastersForcedTrue)).To(Equal(true))
	})

	It("schedulableMastersForcedTrue should set a value when the existing value is nil", func() {
		cluster := createCluster(nil)

		err := clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *cluster.ID)
		Expect(err).ToNot(HaveOccurred())

		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.BoolValue(cluster.SchedulableMastersForcedTrue)).ToNot(BeNil())
	})

	It("schedulableMastersForcedTrue should return an error when the cluster does not exists", func() {
		invalidClusterID := strfmt.UUID(uuid.New().String())
		err := clusterApi.RefreshSchedulableMastersForcedTrue(ctx, invalidClusterID)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("detectAndStoreCollidingIPsForCluster(cluster *common.Cluster, db *gorm.DB)", func() {
	var (
		db         *gorm.DB
		dbName     string
		clusterID  strfmt.UUID
		hostID     strfmt.UUID
		capi       API
		mockEvents *eventsapi.MockHandler
		ctrl       *gomock.Controller
	)

	createCluster := func(l2Connectivity []*models.L2Connectivity) common.Cluster {
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:     &clusterID,
				Status: swag.String(models.ClusterStatusReady),
			},
		}

		hostID = strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID: &hostID,
		}
		var connectivityReport *models.ConnectivityReport = &models.ConnectivityReport{}
		connectivityReport.RemoteHosts = append(connectivityReport.RemoteHosts, &models.ConnectivityRemoteHost{
			HostID:         hostID,
			L2Connectivity: l2Connectivity,
		})
		connectivity, err := hostutil.MarshalConnectivityReport(connectivityReport)
		Expect(err).ShouldNot(HaveOccurred())

		host.Connectivity = connectivity
		Expect(db.Create(&host).Error).ToNot(HaveOccurred())

		cluster.Hosts = append(cluster.Hosts, &host)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		return cluster
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("if no IP collisions are detected then there should be no collisions stored in the DB", func() {
		var l2Connectivity []*models.L2Connectivity
		createCluster(l2Connectivity)
		err := capi.DetectAndStoreCollidingIPsForCluster(clusterID, db)
		Expect(err).ToNot(HaveOccurred())
		var verifyCluster common.Cluster
		err = db.Take(&verifyCluster, "id = ?", clusterID).Error
		Expect(err).ToNot(HaveOccurred())
	})

	It("if ip collisions are detected then there should be collisions stored in the DB", func() {
		var l2Connectivity []*models.L2Connectivity
		l2Connectivity = append(l2Connectivity, &models.L2Connectivity{
			OutgoingIPAddress: "192.168.1.1",
			OutgoingNic:       "eth0",
			RemoteIPAddress:   "192.168.1.2",
			RemoteMac:         "de:ad:be:ef:00:00",
		})
		l2Connectivity = append(l2Connectivity, &models.L2Connectivity{
			OutgoingIPAddress: "192.168.1.1",
			OutgoingNic:       "eth0",
			RemoteIPAddress:   "192.168.1.2",
			RemoteMac:         "be:ef:de:ad:00:00",
		})
		createCluster(l2Connectivity)
		err := capi.DetectAndStoreCollidingIPsForCluster(clusterID, db)
		Expect(err).ToNot(HaveOccurred())
		var verifyCluster common.Cluster
		err = db.Take(&verifyCluster, "id = ?", clusterID).Error
		Expect(err).ToNot(HaveOccurred())
		ipCollisions := make(map[string][]string)
		err = json.Unmarshal([]byte(verifyCluster.Cluster.IPCollisions), &ipCollisions)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(ipCollisions)).To(Equal(1))
		expectedCollision := ipCollisions["192.168.1.2"]
		Expect(expectedCollision).ToNot(BeNil())
		Expect(expectedCollision[0]).To(BeEquivalentTo("be:ef:de:ad:00:00"))
		Expect(expectedCollision[1]).To(BeEquivalentTo("de:ad:be:ef:00:00"))
	})
})

var _ = Describe("UploadEvents", func() {
	var (
		capi               *Manager
		db                 *gorm.DB
		dbName             string
		ctrl               *gomock.Controller
		mockEventsUploader *uploader.MockClient
		mockEventsHandler  *eventsapi.MockHandler
	)
	createCluster := func(status string, uploaded bool) common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:     &clusterID,
				Status: &status,
			},
			Uploaded: uploaded,
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		return cluster
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = eventsapi.NewMockHandler(ctrl)
		mockEventsUploader = uploader.NewMockClient(ctrl)
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, nil, mockEventsHandler, mockEventsUploader, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil, nil)
		mockEventsUploader.EXPECT().IsEnabled().Return(true).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("uploads events when cluster is installed", func() {
		cluster := createCluster(models.ClusterStatusInstalled, false)
		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), gomock.Any(), mockEventsHandler).Return(nil).Times(1)
		capi.UploadEvents()
		clusterAfterUpload, err := common.GetClusterFromDB(db, *cluster.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(clusterAfterUpload).NotTo(BeNil())
		Expect(clusterAfterUpload.Uploaded).To(BeTrue())
	})
	It("uploads events only for clusters that have finished installation", func() {
		cluster1 := createCluster(models.ClusterStatusInsufficient, false)
		cluster2 := createCluster(models.ClusterStatusInstalled, false)
		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), gomock.Any(), mockEventsHandler).Return(nil).Times(1)
		capi.UploadEvents()
		notUploadedCluster, err := common.GetClusterFromDB(db, *cluster1.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(notUploadedCluster).NotTo(BeNil())
		Expect(notUploadedCluster.Uploaded).To(BeFalse())
		uploadedCluster, err := common.GetClusterFromDB(db, *cluster2.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(uploadedCluster).NotTo(BeNil())
		Expect(uploadedCluster.Uploaded).To(BeTrue())
	})
	It("doesn't upload events when there are no clusters", func() {
		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), gomock.Any(), mockEventsHandler).Return(nil).Times(0)
		capi.UploadEvents()
	})
	It("doesn't upload events when there are no clusters that have finished installation", func() {
		cluster1 := createCluster(models.ClusterStatusInsufficient, false)
		cluster2 := createCluster(models.ClusterStatusFinalizing, false)
		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), gomock.Any(), mockEventsHandler).Return(nil).Times(0)
		capi.UploadEvents()
		notUploadedCluster1, err := common.GetClusterFromDB(db, *cluster1.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(notUploadedCluster1).NotTo(BeNil())
		Expect(notUploadedCluster1.Uploaded).To(BeFalse())
		notUploadedCluster2, err := common.GetClusterFromDB(db, *cluster2.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(notUploadedCluster2).NotTo(BeNil())
		Expect(notUploadedCluster2.Uploaded).To(BeFalse())
	})
	It("doesn't upload events when clusters that have finished installing have already been uploaded", func() {
		cluster1 := createCluster(models.ClusterStatusInstalled, true)
		cluster2 := createCluster(models.ClusterStatusCancelled, true)
		mockEventsUploader.EXPECT().UploadEvents(gomock.Any(), gomock.Any(), mockEventsHandler).Return(nil).Times(0)
		capi.UploadEvents()
		notUploadedCluster1, err := common.GetClusterFromDB(db, *cluster1.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(notUploadedCluster1).NotTo(BeNil())
		Expect(notUploadedCluster1.Uploaded).To(BeTrue())
		notUploadedCluster2, err := common.GetClusterFromDB(db, *cluster2.ID, common.SkipEagerLoading)
		Expect(err).NotTo(HaveOccurred())
		Expect(notUploadedCluster2).NotTo(BeNil())
		Expect(notUploadedCluster2.Uploaded).To(BeTrue())
	})
})

var _ = Describe("ResetClusterFiles", func() {
	var (
		ctx               = context.Background()
		db                *gorm.DB
		dbName            string
		clusterID         strfmt.UUID
		hostID            strfmt.UUID
		capi              API
		mockManifestsApi  *manifestsapi.MockManifestsAPI
		mockObjectHandler *s3wrapper.MockAPI
		ctrl              *gomock.Controller
	)

	createCluster := func() common.Cluster {
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:     &clusterID,
				Status: swag.String(models.ClusterStatusReady),
			},
		}
		hostID = strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID: &hostID,
		}
		cluster.Hosts = append(cluster.Hosts, &host)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		return cluster
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		mockManifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		mockObjectHandler = s3wrapper.NewMockAPI(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), nil, nil, nil, nil, nil, dummy, mockOperators, nil, mockObjectHandler, nil, nil, mockManifestsApi)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Should clear only system generated manifests when performing a reset", func() {
		cluster := createCluster()
		clusterPath := filepath.Join(cluster.ID.String()) + "/"
		userManifestPath := filepath.Join(cluster.ID.String(), constants.ManifestFolder, "openshift", "some-user-manifest.yaml")
		systemGeneratedManifestPath := filepath.Join(cluster.ID.String(), constants.ManifestFolder, "manifests", "system-generated-manifest.yaml")
		mockObjectHandler.EXPECT().ListObjectsByPrefix(ctx, clusterPath).Return([]string{userManifestPath, systemGeneratedManifestPath}, nil).Times(1)
		mockManifestsApi.EXPECT().IsUserManifest(ctx, *cluster.ID, "openshift", "some-user-manifest.yaml").Return(true, nil).Times(1)
		mockManifestsApi.EXPECT().IsUserManifest(ctx, *cluster.ID, "manifests", "system-generated-manifest.yaml").Return(false, nil).Times(1)
		mockObjectHandler.EXPECT().DeleteObject(ctx, systemGeneratedManifestPath).Times(1)
		mockObjectHandler.EXPECT().DeleteObject(ctx, userManifestPath).Times(0)
		Expect(capi.ResetClusterFiles(ctx, &cluster, mockObjectHandler)).To(BeNil())
	})

	It("Should delete all non manifest files on cluster reset, with the exception of logs", func() {
		cluster := createCluster()
		clusterPath := filepath.Join(cluster.ID.String()) + "/"
		logFilePath := filepath.Join(cluster.ID.String(), "logs", "somelog.txt")
		fileToBeDeletedPath := filepath.Join(cluster.ID.String(), "something.ign")
		mockObjectHandler.EXPECT().ListObjectsByPrefix(ctx, clusterPath).Return([]string{logFilePath, fileToBeDeletedPath}, nil).Times(1)
		mockObjectHandler.EXPECT().DeleteObject(ctx, fileToBeDeletedPath).Times(1)
		mockObjectHandler.EXPECT().DeleteObject(ctx, logFilePath).Times(0)
		Expect(capi.ResetClusterFiles(ctx, &cluster, mockObjectHandler)).To(BeNil())
	})
})
