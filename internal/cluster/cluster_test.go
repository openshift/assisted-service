package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
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
		ctx              = context.Background()
		db               *gorm.DB
		state            API
		cluster          *common.Cluster
		refreshedCluster *common.Cluster
		stateErr         error
		dbName           string
		mockOperators    *operators.MockAPI
		mockS3Client     *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl := gomock.NewController(GinkgoT())
		mockOperators = operators.NewMockAPI(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, nil, nil, nil, nil, dummy, mockOperators, nil, mockS3Client, nil, nil)
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
			}, nil)
		})

		It("update_cluster", func() {
			refreshedCluster, stateErr = state.RefreshStatus(ctx, cluster, db)
		})

		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
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
		db                *gorm.DB
		c                 common.Cluster
		id                strfmt.UUID
		err               error
		clusterApi        *Manager
		shouldHaveUpdated bool
		expectedState     string
		ctrl              *gomock.Controller
		mockHostAPI       *host.MockAPI
		mockMetric        *metrics.MockAPI
		dbName            string
		mockEvents        *eventsapi.MockHandler
		mockS3Client      *s3wrapper.MockAPI
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
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, mockS3Client, nil, nil)
		expectedState = ""
		shouldHaveUpdated = false

		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
		}, nil)
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
			It("with insufficien working workers count, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with insufficien working workers count, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with single worker in error, installing -> error", func() {
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createHost(id, "installing", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
			It("with single worker in error, installing -> error", func() {
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createHost(id, "installed", db)
				createWorkerHost(id, "error", db)
				shouldHaveUpdated = true
				expectedState = "error"
			})
		})
		Context("from finalizing state", func() {
			BeforeEach(func() {
				c = createCluster(&id, models.ClusterStatusFinalizing, statusInfoFinalizing)
				createHost(id, models.ClusterStatusInstalled, db)
				createHost(id, models.ClusterStatusInstalled, db)
				createHost(id, models.ClusterStatusInstalled, db)
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
		})

		Context("from installed state", func() {
			BeforeEach(func() {
				c = createCluster(&id, models.ClusterStatusInstalled, statusInfoInstalled)
				mockMetric.EXPECT().MonitoredClusterCount(gomock.Any()).AnyTimes()
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			})

			It("installed -> installed", func() {
				createHost(id, models.ClusterStatusInstalled, db)
				createHost(id, models.ClusterStatusInstalled, db)
				createHost(id, models.ClusterStatusInstalled, db)
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
							APIVip:          common.TestIPv4Networking.APIVip,
							IngressVip:      common.TestIPv4Networking.IngressVip,
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
					Expect(db.Model(&c).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.6"}).Error).To(Not(HaveOccurred()))
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
							APIVip:          common.TestIPv4Networking.APIVip,
							IngressVip:      common.TestIPv4Networking.IngressVip,
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
					APIVip:          common.TestIPv4Networking.APIVip,
					IngressVip:      common.TestIPv4Networking.IngressVip,
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

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("lease timeout event", func() {
	var (
		db          *gorm.DB
		c           common.Cluster
		id          strfmt.UUID
		clusterApi  *Manager
		ctrl        *gomock.Controller
		mockHostAPI *host.MockAPI
		mockMetric  *metrics.MockAPI
		dbName      string
		mockEvents  *eventsapi.MockHandler
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil)

		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
		}, nil)
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
			eventCalllsExpected: 1,
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
				APIVip:            t.apiVip,
				IngressVip:        t.ingressVip,
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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil)

		mockMetric.EXPECT().MonitoredClusterCount(int64(1)).AnyTimes()
		mockMetric.EXPECT().Duration("ClusterMonitoring", gomock.Any()).AnyTimes()
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied)},
		}, nil)
	})
	tests := []struct {
		name                    string
		srcState                string
		machineNetworkCIDR      string
		expectedMachineCIDR     string
		apiVip                  string
		hosts                   []*models.Host
		eventCallExpected       bool
		userActionResetExpected bool
		dhcpEnabled             bool
		userManagedNetworking   bool
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
			machineNetworkCIDR:      "192.168.0.0/16",
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
			machineNetworkCIDR:      "192.168.0.0/16",
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
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
			machineNetworkCIDR:      "1.2.3.0/24",
			expectedMachineCIDR:     "1.2.3.0/24",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
		},
		{
			name:                    "No hosts, machine cidr already set - dhcp disabled",
			srcState:                models.ClusterStatusPendingForInput,
			userActionResetExpected: true,
			eventCallExpected:       true,
			machineNetworkCIDR:      "192.168.0.0/16",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
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
			machineNetworkCIDR:      "192.168.0.0/16",
			dhcpEnabled:             false,
			apiVip:                  "1.2.3.8",
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
			machineNetworkCIDR:      "192.168.0.0/16",
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
				APIVip:                t.apiVip,
				ClusterNetworks:       common.TestIPv4Networking.ClusterNetworks,
				ServiceNetworks:       common.TestIPv4Networking.ServiceNetworks,
				MachineNetworks:       network.CreateMachineNetworksArray(t.machineNetworkCIDR),
				VipDhcpAllocation:     swag.Bool(t.dhcpEnabled),
				UserManagedNetworking: swag.Bool(t.userManagedNetworking),
			}}
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

			ctrl.Finish()
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
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
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl := gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
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

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("VerifyClusterUpdatability", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, cluster can be updated only in one of [insufficient ready pending-for-input adding-hosts]"
		dbName      string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		ctrl := gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			nil, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
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

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
		eventsHandler = events.New(db, logrus.New())
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     swag.String(models.ClusterStatusInsufficient),
			StatusInfo: swag.String(StatusInfoInsufficient)}}
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			c.Status = swag.String(models.ClusterStatusInstalling)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusCancelled, models.ClusterStatusInstalling, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, c.ID, nil, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			Expect(*cancelEvent.Message).Should(Equal("Canceled cluster installation"))
		})
		It("cancel_failed_installation", func() {
			c.Status = swag.String(models.ClusterStatusError)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusCancelled, models.ClusterStatusError, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, c.ID, nil, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
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
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusCancelled, models.ClusterStatusInsufficient, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).Should(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, c.ID, nil, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		ctrl := gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
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
		events, err := eventsHandler.V2GetEvents(ctx, c.ID, nil, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
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
		events, err := eventsHandler.V2GetEvents(ctx, c.ID, nil, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityError))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
	Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.6"}).Error).To(Not(HaveOccurred()))
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
	Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.6"}).Error).To(Not(HaveOccurred()))
}

func defaultInventoryWithTimestamp(timestamp int64) string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Timestamp: timestamp,
		CPU: &models.CPU{
			Count: 16,
		},
		Memory: &models.Memory{
			UsableBytes: 64000000000,
		},
		Disks: []*models.Disk{
			{
				SizeBytes: 20000000000,
				DriveType: "HDD",
			}, {
				SizeBytes: 40000000000,
				DriveType: "SSD",
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
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEventsHandler, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
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

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("HandlePreInstallationChanges", func() {
	var (
		ctx        = context.Background()
		capi       API
		db         *gorm.DB
		clusterId  strfmt.UUID
		dbName     string
		mockEvents *eventsapi.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl := gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEvents, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		cluster := &common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusPreparingForInstallation)}}
		Expect(db.Create(cluster).Error).ShouldNot(HaveOccurred())
	})

	It("HandlePreInstallError", func() {
		var cluster common.Cluster
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		capi.HandlePreInstallError(ctx, &cluster, errors.Errorf("pre-install error"))
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(cluster.InstallationPreparationCompletionStatus).Should(Equal(common.InstallationPreparationFailed))
	})

	It("HandlePreInstallSuccess", func() {
		var cluster common.Cluster
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
		capi.HandlePreInstallSuccess(ctx, &cluster)
		Expect(db.Take(&cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(cluster.InstallationPreparationCompletionStatus).Should(Equal(common.InstallationPreparationSucceeded))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEvents, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
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
			cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.srcState)}}
			cluster.APIVip = t.clusterApiVip
			cluster.IngressVip = t.clusterIngressVip
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			if t.eventExpected {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ApiIngressVipUpdatedEventName),
					eventstest.WithClusterIdMatcher(clusterId.String()))).Times(1)
			}
			err := capi.SetVipsData(ctx, &cluster, t.apiVip, t.ingressVip, t.clusterApiLease, t.clusterIngressLease, db)
			Expect(err != nil).To(Equal(t.errorExpected))
			var c common.Cluster
			Expect(db.Take(&c, "id = ?", clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(c.APIVip).To(Equal(t.expectedApiVip))
			Expect(c.IngressVip).To(Equal(t.expectedIngressVip))
			Expect(c.ApiVipLease).To(Equal(t.expectedApiLease))
			Expect(c.IngressVipLease).To(Equal(t.expectedIngressLease))
			Expect(swag.StringValue(c.Status)).To(Equal(t.expectedState))
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
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

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dbIndex++
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockOperators := operators.NewMockAPI(ctrl)
		mockMetricApi = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, nil, mockMetricApi, nil, dummy, mockOperators, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(models.ClusterStatusReady),
			ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
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
		}, nil)
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(models.ClusterStatusReady),
			ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
			ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
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
		}, nil)
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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:              &id,
			Status:          swag.String(currentState),
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVip:          common.TestIPv4Networking.APIVip,
			IngressVip:      common.TestIPv4Networking.IngressVip,
			BaseDNSDomain:   "test.com",
			PullSecretSet:   true,
		}}
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName),
			eventstest.WithClusterIdMatcher(id.String()))).AnyTimes()
	})

	AfterEach(func() {
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
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).NotTo(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := eventsapi.NewMockHandler(ctrl)
		mockOperators = operators.NewMockAPI(ctrl)
		mockOperators.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		dummy := &leader.DummyElector{}
		capi = NewManager(cfg, common.GetTestLog(), db, mockEvents, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
	})

	It("no change", func() {
		Expect(db.Take(&cl, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(*refreshedCluster.Status).To(Equal(models.ClusterStatusPreparingForInstallation))
	})

	It("timeout", func() {
		Expect(db.Model(&cl).Update("status_updated_at", strfmt.DateTime(time.Now().Add(-15*time.Minute))).Error).
			NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(refreshedCluster.Status)).To(Equal(models.ClusterStatusReady))
	})

	AfterEach(func() {
		common.CloseDB(db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Cluster tarred files", func() {
	var (
		ctx          = context.Background()
		capi         API
		db           *gorm.DB
		clusterId    strfmt.UUID
		cl           common.Cluster
		dbName       string
		ctrl         *gomock.Controller
		mockHostAPI  *host.MockAPI
		mockS3Client *s3wrapper.MockAPI
		prefix       string
		files        []string
		tarFile      string
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cfg := Config{}
		files = []string{"test", "test2"}
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockOperators := operators.NewMockAPI(ctrl)
		capi = NewManager(cfg, common.GetTestLog(), db, mockEvents, mockHostAPI, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		tarFile = fmt.Sprintf("%s/logs/cluster_logs.tar", clusterId)
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
		prefix = fmt.Sprintf("%s/logs/", cl.ID)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("list objects failed", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(nil, errors.Errorf("dummy"))
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects no files", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{tarFile}, nil)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects only all logs file", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{}, nil)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("download failed", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("upload failed", func() {
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, gomock.Any()).Return(r, int64(4), nil).AnyTimes()
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), tarFile).Return(errors.Errorf("Dummy")).Times(1)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
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
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		mockOperatorMgr = operators.NewMockAPI(ctrl)
		cfg := getDefaultConfig()
		capi = NewManager(cfg, common.GetTestLog(), db, eventsHandler, nil, mockMetric, manifestsGenerator, dummy, mockOperatorMgr, nil, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusReady),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
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
		capi = NewManager(cfg2, common.GetTestLog(), db, eventsHandler, nil, mockMetric, manifestsGenerator, nil, mockOperatorMgr, nil, nil, nil, nil)
		manifestsGenerator.EXPECT().AddChronyManifest(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		manifestsGenerator.EXPECT().IsSNODNSMasqEnabled().Return(false).Times(1)
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
			capi = NewManager(telemeterCfg, common.GetTestLog(), db, eventsHandler, nil, mockMetric, manifestsGenerator, nil, mockOperatorMgr, nil, nil, nil, nil)
		})

		It("Happy flow", func() {

			manifestsGenerator.EXPECT().AddChronyManifest(ctx, gomock.Any(), &c).Return(nil)
			mockOperatorMgr.EXPECT().GenerateManifests(ctx, &c).Return(nil)
			manifestsGenerator.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), &c).Return(nil)
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

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, mockMetric, nil, dummy, mockOperators, nil, mockS3Client, nil, nil)
		c = registerCluster()
	})

	It("Deregister inactive cluster", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
		Expect(state.DeregisterInactiveCluster(ctx, 10, strfmt.DateTime(time.Now()))).ShouldNot(HaveOccurred())
		Expect(wasDeregisterd(db, *c.ID)).To(BeTrue())
	})

	It("Do noting, active cluster", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Times(0)
		lastActive := strfmt.DateTime(time.Now().Add(-time.Hour))
		Expect(state.DeregisterInactiveCluster(ctx, 10, lastActive)).ShouldNot(HaveOccurred())
		Expect(wasDeregisterd(db, *c.ID)).To(BeFalse())
	})

	It("Deregister inactive cluster with new clusters", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(4)
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
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(3)
		inactiveCluster1 := registerCluster()
		inactiveCluster2 := registerCluster()
		inactiveCluster3 := registerCluster()
		inactiveCluster4 := registerCluster()
		inactiveCluster5 := registerCluster()
		inactiveCluster6 := registerCluster()

		lastActive := strfmt.DateTime(time.Now())

		Expect(state.DeregisterInactiveCluster(ctx, 3, lastActive)).ShouldNot(HaveOccurred())

		Expect(wasDeregisterd(db, *inactiveCluster1.ID)).To(BeTrue())
		Expect(wasDeregisterd(db, *inactiveCluster2.ID)).To(BeTrue())

		Expect(wasDeregisterd(db, *inactiveCluster3.ID)).To(BeFalse())
		Expect(wasDeregisterd(db, *inactiveCluster4.ID)).To(BeFalse())
		Expect(wasDeregisterd(db, *inactiveCluster5.ID)).To(BeFalse())
		Expect(wasDeregisterd(db, *inactiveCluster6.ID)).To(BeFalse())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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

		clusterEvents, err := eventsHandler.V2GetEvents(ctx, &clusterID, nil, nil)
		if isDeleted {
			Expect(err).Should(HaveOccurred())
			Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
		} else {
			Expect(err).ShouldNot(HaveOccurred())
		}
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
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, mockMetric, nil, dummy, mockOperators, nil, nil, nil, nil)
		c1 = registerCluster()
		c2 = registerCluster()
		c3 = registerCluster()
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

		Expect(state.PermanentClustersDeletion(ctx, strfmt.DateTime(time.Now()), mockS3Api)).ShouldNot(HaveOccurred())

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

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, nil, nil, dummy, mockOperators, nil, nil, nil, nil)
		key = types.NamespacedName{
			Namespace: kubeKeyNamespace,
			Name:      kubeKeyName,
		}
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

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog(), db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	tests := []struct {
		name          string
		clusterStatus string
		clusterKind   string
		errorExpected bool
	}{
		{
			name:          "successfully transform day1 cluster to a day2 cluster - status installed",
			clusterStatus: models.ClusterStatusInstalled,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: false,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - kind AddHostsCluster",
			clusterStatus: models.ClusterStatusInstalled,
			clusterKind:   models.ClusterKindAddHostsCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status insufficient",
			clusterStatus: models.ClusterStatusInsufficient,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status ready",
			clusterStatus: models.ClusterStatusReady,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status error",
			clusterStatus: models.ClusterStatusError,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - preparing-for-installation",
			clusterStatus: models.ClusterStatusPreparingForInstallation,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status pending-for-input",
			clusterStatus: models.ClusterStatusPendingForInput,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status installing",
			clusterStatus: models.ClusterStatusInstalling,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status finalizing",
			clusterStatus: models.ClusterStatusFinalizing,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status adding-hosts",
			clusterStatus: models.ClusterStatusAddingHosts,
			clusterKind:   models.ClusterKindAddHostsCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status cancelled",
			clusterStatus: models.ClusterStatusCancelled,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
		{
			name:          "fail to transform day1 cluster to a day2 cluster - status installing-pending-user-action",
			clusterStatus: models.ClusterStatusInstallingPendingUserAction,
			clusterKind:   models.ClusterKindCluster,
			errorExpected: true,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			id := strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{
				ID:               &id,
				Kind:             swag.String(t.clusterKind),
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Status:           swag.String(t.clusterStatus),
				MachineNetworks:  common.TestIPv4Networking.MachineNetworks,
				APIVip:           common.TestIPv4Networking.APIVip,
				IngressVip:       common.TestIPv4Networking.IngressVip,
				BaseDNSDomain:    "test.com",
				PullSecretSet:    true,
			}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			err1 := clusterApi.TransformClusterToDay2(ctx, cluster, db)
			Expect(err1 != nil).To(Equal(t.errorExpected))
			if !t.errorExpected {
				var c common.Cluster
				Expect(db.Take(&c, "id = ?", cluster.ID.String()).Error).ToNot(HaveOccurred())
				Expect(c.Kind).To(Equal(swag.String(models.ClusterKindAddHostsCluster)))
				Expect(c.Status).To(Equal(swag.String(models.ClusterStatusAddingHosts)))
				apiVipDnsname := fmt.Sprintf("api.%s.%s", c.Name, c.BaseDNSDomain)
				Expect(c.APIVipDNSName).To(Equal(swag.String(apiVipDnsname)))
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
		eventsHandler = events.New(db, logrus.New())
		api = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
		m = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEvents, mockHost, mockMetric, nil, nil, nil, nil, mockS3Client, nil, nil)
		c = registerTestClusterWithValidationsAndHost()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Test DeregisterCluster before discovery image was generated", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Times(0)
		mockHost.EXPECT().ReportValidationFailedMetrics(ctx, gomock.Any(), openshiftVersion, emailDomain)
		mockMetric.EXPECT().ClusterValidationFailed(openshiftVersion, emailDomain, models.ClusterValidationIDSufficientMastersCount)
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterDeregisteredEventName),
			eventstest.WithClusterIdMatcher(c.ID.String())))
		err := m.DeregisterCluster(ctx, c)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Test DeregisterCluster after discovery image was generated", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHost.EXPECT().ReportValidationFailedMetrics(ctx, gomock.Any(), openshiftVersion, emailDomain)
		mockMetric.EXPECT().ClusterValidationFailed(openshiftVersion, emailDomain, models.ClusterValidationIDSufficientMastersCount)
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterDeregisteredEventName),
			eventstest.WithClusterIdMatcher(c.ID.String())))
		err := m.DeregisterCluster(ctx, c)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Test reportValidationStatusChanged", func() {
		mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithClusterIdMatcher(c.ID.String())))
		newValidationRes := generateTestValidationResult(ValidationSuccess)
		var currentValidationRes ValidationsStatus
		err := json.Unmarshal([]byte(c.ValidationsInfo), &currentValidationRes)
		Expect(err).ToNot(HaveOccurred())
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)

		mockMetric.EXPECT().ClusterValidationChanged(openshiftVersion, emailDomain, models.ClusterValidationIDSufficientMastersCount)
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
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
