package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/common/testing"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/uploader"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("Transition tests", func() {
	var (
		ctx              = context.Background()
		capi             API
		db               *gorm.DB
		clusterId        strfmt.UUID
		eventsHandler    eventsapi.Handler
		uploadClient     uploader.Client
		ctrl             *gomock.Controller
		mockMetric       *metrics.MockAPI
		dbName           string
		operatorsManager *operators.MockAPI
		mockS3Api        *s3wrapper.MockAPI
		mockAccountsMgmt *ocm.MockOCMAccountsMgmt
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = events.New(db, nil, testing.GetDummyNotificationStream(ctrl), logrus.New())
		uploadClient = uploader.NewClient(&uploader.Config{EnableDataCollection: false}, nil, logrus.New(), nil)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockAccountsMgmt = ocm.NewMockOCMAccountsMgmt(ctrl)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("cancel_installation", func() {
		BeforeEach(func() {
			capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, testing.GetDummyNotificationStream(ctrl), eventsHandler, uploadClient, nil, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)
		})

		It("cancel_installation", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusCancelled, models.ClusterStatusInstalling, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
			Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
		})

		It("cancel_installation_conflict", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusInsufficient)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			replay := capi.CancelInstallation(ctx, &c, "", db)
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
		})

		It("cancel_failed_installation", func() {
			c := common.Cluster{
				Cluster: models.Cluster{
					ID:         &clusterId,
					StatusInfo: swag.String("original error"),
					Status:     swag.String(models.ClusterStatusError)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
			Expect(swag.StringValue(c.StatusInfo)).ShouldNot(Equal("original error"))
		})
	})
	Context("complete_installation", func() {
		tests := []struct {
			name                         string
			operators                    []*models.MonitoredOperator
			uploadKubeConfig             bool
			updateAMSSubscription        bool
			updateAMSSubscriptionSuccess bool
			errorExpected                bool
			updateSuccessfullyFinished   bool
			withWorkers                  bool
			withWorkersInstalled         bool
			destState                    string
			destStatusInfo               string
		}{
			{
				name:      "no change -> finalizing",
				destState: models.ClusterStatusFinalizing,
			},
			// TODO: MGMT-4458
			// Backward-compatible solution for clusters that don't have monitored operators data
			// Those clusters shouldn't finish until the controller would tell them.
			{
				name:                       "no operators (backward-compatability) -> installed",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				destState:                  models.ClusterStatusInstalled,
				destStatusInfo:             statusInfoInstalled,
			},
			{
				name:                       "available builtin operators, no OLM -> installed",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
				},
				destState:      models.ClusterStatusInstalled,
				destStatusInfo: statusInfoInstalled,
			},
			{
				name:                       "progressing builtin operator, no OLM -> finalizing",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: false,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusProgressing,
					},
				},

				destState: models.ClusterStatusFinalizing,
			},
			{
				name:                       "failed builtin operator, no OLM -> finalizing",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: false,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusFailed,
					},
				},

				destState: models.ClusterStatusFinalizing,
			},
			{
				name:                       "available builtin operators, progress OLM -> finalizing",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: false,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "2", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusProgressing,
					},
				},
				destState: models.ClusterStatusFinalizing,
			},
			{
				name:                       "available builtin operators, failed OLM -> installed (degraded)",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "2", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusFailed,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "3", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusAvailable,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "4", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusFailed,
					},
				},
				destState:      models.ClusterStatusInstalled,
				destStatusInfo: StatusInfoDegraded + ". Failed OLM operators: dummy2, dummy4",
			},
			{
				name:                       "no operators, extra worker in error -> installed (with warning)",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators:                  []*models.MonitoredOperator{},
				withWorkers:                true,
				withWorkersInstalled:       false,
				destState:                  models.ClusterStatusInstalled,
				destStatusInfo:             StatusInfoNotAllWorkersInstalled,
			},
			{
				name:                       "available builtin operators, available OLM, extra worker in error -> installed (with warning)",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "2", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusAvailable,
					},
				},
				withWorkers:          true,
				withWorkersInstalled: false,
				destState:            models.ClusterStatusInstalled,
				destStatusInfo:       StatusInfoNotAllWorkersInstalled,
			},
			{
				name:                       "available builtin operators, available OLM, all workers installed -> installed",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name + "2", OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusAvailable,
					},
				},
				withWorkers:          true,
				withWorkersInstalled: true,
				destState:            models.ClusterStatusInstalled,
				destStatusInfo:       statusInfoInstalled,
			},
			{
				name:                         "success - with AMS -> installed",
				uploadKubeConfig:             true,
				updateSuccessfullyFinished:   true,
				updateAMSSubscription:        true,
				updateAMSSubscriptionSuccess: true,
				destState:                    models.ClusterStatusInstalled,
				destStatusInfo:               statusInfoInstalled,
			},
			{
				name:                         "success - with AMS (update failed) -> finalizing",
				uploadKubeConfig:             true,
				updateSuccessfullyFinished:   true,
				updateAMSSubscription:        true,
				updateAMSSubscriptionSuccess: false,
				errorExpected:                true,
				destState:                    models.ClusterStatusFinalizing,
			},
		}

		findEventByMessage := func(events []*common.Event, eventMessage string) *common.Event {
			for _, event := range events {
				if swag.StringValue(event.Message) == eventMessage {
					return event
				}
			}
			return nil
		}

		checkCompleteInstallationUpdate := func(eventSeverity string, eventMessage string) {
			response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(&clusterId, nil, nil))
			Expect(err).ShouldNot(HaveOccurred())
			events := response.GetEvents()
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := findEventByMessage(events, eventMessage)
			Expect(resetEvent).ToNot(BeNil())
			Expect(*resetEvent.Severity).Should(Equal(eventSeverity))

			if eventMessage != "" {
				Expect(funk.Contains(*resetEvent.Message, eventMessage)).Should(Equal(true))
			}

			var clusterInfo common.Cluster
			db.First(&clusterInfo)
			completionTime := time.Time(clusterInfo.InstallCompletedAt).In(time.UTC)
			Expect(time.Until(completionTime)).Should(BeNumerically("<", 1*time.Second))
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				// Setup
				c := common.Cluster{
					Cluster: models.Cluster{
						ID:                 &clusterId,
						Status:             swag.String(models.ClusterStatusFinalizing),
						MonitoredOperators: t.operators,
						StatusUpdatedAt:    strfmt.DateTime(time.Now()),
					},
					IsAmsSubscriptionConsoleUrlSet: true,
				}
				Expect(common.LoadTableFromDB(db, common.MonitoredOperatorsTable).Create(&c).Error).ShouldNot(HaveOccurred())
				if t.withWorkers {
					for i := 0; i < MinMastersNeededForInstallation; i++ {
						createHost(clusterId, models.HostStatusInstalled, db)
					}
					for i := 0; i < 2; i++ {
						createWorkerHost(clusterId, models.HostStatusInstalled, db)
					}
					if t.withWorkersInstalled {
						createWorkerHost(clusterId, models.HostStatusInstalled, db)
					} else {
						createWorkerHost(clusterId, models.HostStatusError, db)
					}
				}

				var ocmClient *ocm.Client = nil

				if t.updateAMSSubscription {
					ocmClient = &ocm.Client{AccountsMgmt: mockAccountsMgmt, Config: &ocm.Config{}}
				}

				if t.updateSuccessfullyFinished {
					if t.updateAMSSubscription {
						if t.updateAMSSubscriptionSuccess {
							mockAccountsMgmt.EXPECT().UpdateSubscriptionStatusActive(gomock.Any(), gomock.Any()).Return(nil).Times(1)
						} else {
							mockAccountsMgmt.EXPECT().UpdateSubscriptionStatusActive(gomock.Any(), gomock.Any()).Return(errors.New("error")).Times(1)
						}
					}
				}

				mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(t.uploadKubeConfig, nil).AnyTimes() // Might be affected by the amount of states

				if t.updateSuccessfullyFinished {
					mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusInstalled, models.ClusterStatusFinalizing, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				}

				capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, testing.GetDummyNotificationStream(ctrl), eventsHandler, uploadClient, nil, mockMetric, nil, nil, operatorsManager, ocmClient, mockS3Api, nil, nil, nil, false)

				// Test
				clusterAfterRefresh, err := capi.RefreshStatus(ctx, &c, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(swag.StringValue(clusterAfterRefresh.Status)).To(Equal(t.destState))
					Expect(swag.StringValue(clusterAfterRefresh.StatusInfo)).To(Equal(t.destStatusInfo))

					if t.destState != *c.Status {
						if t.updateSuccessfullyFinished {
							checkCompleteInstallationUpdate(models.EventSeverityInfo, "Successfully completed installing cluster")
						} else {
							checkCompleteInstallationUpdate(models.EventSeverityCritical, fmt.Sprintf("Operator %s failed", t.operators[0].Name))
						}
					}
				}

				// Check also the DB
				clusterFromDB := getClusterFromDB(clusterId, db)
				Expect(swag.StringValue(clusterFromDB.Status)).To(Equal(t.destState))
				Expect(swag.StringValue(clusterFromDB.StatusInfo)).To(Equal(t.destStatusInfo))
			})
		}
	})
})

var _ = Describe("Cancel cluster installation", func() {
	var (
		ctx               = context.Background()
		dbName            string
		capi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEventsHandler *eventsapi.MockHandler
		mockMetric        *metrics.MockAPI
		uploadClient      uploader.Client
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = eventsapi.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		uploadClient = uploader.NewClient(&uploader.Config{EnableDataCollection: false}, nil, logrus.New(), nil)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, testing.GetDummyNotificationStream(ctrl), mockEventsHandler, uploadClient, nil, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).Times(times)
	}

	acceptClusterInstallationFinished := func() {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	}

	installationStates := []string{
		models.ClusterStatusPreparingForInstallation, models.ClusterStatusInstalling, models.ClusterStatusFinalizing}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
		eventsNum  int
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true, eventsNum: 2},
		{state: models.ClusterStatusInstalling, success: true, eventsNum: 2},
		{state: models.ClusterStatusError, success: true, eventsNum: 2},
		{state: models.ClusterStatusFinalizing, success: true, eventsNum: 2},
		{state: models.ClusterStatusInstallingPendingUserAction, success: true, eventsNum: 2},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict, eventsNum: 1},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict, eventsNum: 1},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict, eventsNum: 1},
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			acceptNewEvents(t.eventsNum)
			if funk.Contains(installationStates, t.state) {
				acceptClusterInstallationFinished()
			}
			err := capi.CancelInstallation(ctx, &cluster, "reason", db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
			}
		})
	}
})

var _ = Describe("Reset cluster", func() {
	var (
		ctx               = context.Background()
		dbName            string
		capi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEventsHandler *eventsapi.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = eventsapi.NewMockHandler(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, testing.GetDummyNotificationStream(ctrl), mockEventsHandler, nil, nil, nil, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).Times(times)
	}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
		eventsNum  int
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true, eventsNum: 2},
		{state: models.ClusterStatusInstalling, success: true, eventsNum: 2},
		{state: models.ClusterStatusError, success: true, eventsNum: 2},
		{state: models.ClusterStatusFinalizing, success: true, eventsNum: 2},
		{state: models.ClusterStatusInstallingPendingUserAction, success: true, eventsNum: 2},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict, eventsNum: 1},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict, eventsNum: 1},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict, eventsNum: 1},
	}

	for _, t := range tests {
		t := t
		clusterId := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
		}
		It(fmt.Sprintf("resets cluster from state %s", t.state), func() {
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			acceptNewEvents(t.eventsNum)
			err := capi.ResetCluster(ctx, &cluster, "reason", db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
			}
		})
		It(fmt.Sprintf("resets API VIP and Ingress VIP in case of single node cluster from state %s", t.state), func() {
			haMode := models.ClusterHighAvailabilityModeNone
			hostIP := "1.2.3.4"
			cluster = common.Cluster{
				Cluster: models.Cluster{
					ID:                   &clusterId,
					Status:               swag.String(t.state),
					HighAvailabilityMode: &haMode,
					APIVips:              []*models.APIVip{{IP: models.IP(hostIP)}},
					IngressVips:          []*models.IngressVip{{IP: models.IP(hostIP)}},
				},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			acceptNewEvents(t.eventsNum)
			err := capi.ResetCluster(ctx, &cluster, "reason", db)
			cluster = getClusterFromDB(clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(cluster.Cluster.APIVips)).Should(Equal(0))
				Expect(len(cluster.Cluster.IngressVips)).Should(Equal(0))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
				Expect(len(cluster.Cluster.APIVips)).ShouldNot(Equal(0))
				Expect(len(cluster.Cluster.IngressVips)).ShouldNot(Equal(0))
			}
		})
	}
})

type statusInfoChecker interface {
	check(statusInfo *string)
}

type valueChecker struct {
	value string
}

func (v *valueChecker) check(value *string) {
	if value == nil {
		Expect(v.value).To(Equal(""))
	} else {
		Expect(*value).To(Equal(v.value))
	}
}

func makeValueChecker(value string) statusInfoChecker {
	return &valueChecker{value: value}
}

type validationsChecker struct {
	expected map[ValidationID]validationCheckResult
}

func (j *validationsChecker) check(validationsStr string) {
	validationRes := make(ValidationsStatus)
	Expect(json.Unmarshal([]byte(validationsStr), &validationRes)).ToNot(HaveOccurred())
next:
	for id, checkedResult := range j.expected {
		category, err := id.Category()
		Expect(err).ToNot(HaveOccurred())
		results, ok := validationRes[category]
		Expect(ok).To(BeTrue())
		for _, r := range results {
			if r.ID == id {
				Expect(r.Status).To(Equal(checkedResult.status), "id = %s", id.String())
				Expect(r.Message).To(MatchRegexp(checkedResult.messagePattern))
				continue next
			}
		}
		// Should not reach here
		Expect(false).To(BeTrue())
	}
}

type validationCheckResult struct {
	status         ValidationStatus
	messagePattern string
}

func makeJsonChecker(expected map[ValidationID]validationCheckResult) *validationsChecker {
	return &validationsChecker{expected: expected}
}

var _ = Describe("Refresh Cluster - No DHCP", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *eventsapi.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string
		mockS3Api                               *s3wrapper.MockAPI
	)

	type candidateChecker func()

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	checkMasterCandidates := func(times int) candidateChecker {
		return func() {
			mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(times)
		}
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			srcStatusInfo      string
			machineNetworks    []*models.MachineNetwork
			apiVips            []*models.APIVip
			ingressVips        []*models.IngressVip
			dnsDomain          string
			pullSecretSet      bool
			dstState           string
			hosts              []models.Host
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			candidateChecker   candidateChecker
			errorExpected      bool
		}{
			{
				name:          "pending-for-input to pending-for-input",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusPendingForInput,
				apiVips:       nil,
				ingressVips:   nil,
				dnsDomain:     "test.com",
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined and must be provided"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:          "pending-for-input to pending-for-input api-vip-defined",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusPendingForInput,
				apiVips:       common.TestIPv4Networking.APIVips,
				ingressVips:   nil,
				dnsDomain:     "test.com",
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to pending-for-input machine-cidr defined",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         nil,
				ingressVips:     nil,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined and must be provided"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to pending-for-input api-vip and machine-cidr defined",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     nil,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:          "pending-for-input to pending-for-input with single inventory",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusPendingForInput,
				apiVips:       nil,
				ingressVips:   nil,
				dnsDomain:     "test.com",
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster, Inventory: common.GenerateTestDefaultInventory()},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "The Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined and must be provided"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:          "pending-for-input to pending-for-input with 2 workers",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusPendingForInput,
				apiVips:       nil,
				ingressVips:   nil,
				dnsDomain:     "test.com",
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined."},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined and must be provided"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationSuccess,
						messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:          "pending-for-input to pending-for-input with 3 master 2 workers candidates in auto-assign mode",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusPendingForInput,
				apiVips:       nil,
				ingressVips:   nil,
				dnsDomain:     "test.com",
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
				},
				candidateChecker:  checkMasterCandidates(3),
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "The Machine Network CIDR is undefined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined."},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined and must be provided"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationFailure, messagePattern: "Ingress virtual IPs are undefined and must be provided"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationSuccess,
						messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - masters > 3",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - worker = 1 with auto-assign",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
				},
				candidateChecker:  checkMasterCandidates(3),
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - worker = 1",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - not all hosts are ready to install",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to pending-for-input - api vip not defined",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         nil,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to pending-for-input - dns domain not defined",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationFailure, messagePattern: "The base domain is undefined and must be provided"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to pending-for-input - pull secret not set",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   false,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationFailure, messagePattern: "The pull secret is not set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			}, {
				name:            "pending-for-input to ready",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "insufficient to ready",
				srcState:        models.ClusterStatusInsufficient,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to ready",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "finalizing to finalizing",
				srcState:        models.ClusterStatusFinalizing,
				srcStatusInfo:   statusInfoFinalizing,
				dstState:        models.ClusterStatusFinalizing,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoFinalizing),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "error to error",
				srcState:        models.ClusterStatusError,
				dstState:        models.ClusterStatusError,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "installed to installed",
				srcState:        models.ClusterStatusInstalled,
				srcStatusInfo:   statusInfoInstalled,
				dstState:        models.ClusterStatusInstalled,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoInstalled),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "Installing to Installing",
				srcState:        models.ClusterStatusInstalling,
				srcStatusInfo:   statusInfoInstalling,
				dstState:        models.ClusterStatusInstalling,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoInstalling),
				validationsChecker: nil,
				errorExpected:      false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVips:         t.apiVips,
						ID:              &clusterId,
						IngressVips:     t.ingressVips,
						MachineNetworks: t.machineNetworks,
						Status:          &t.srcState,
						StatusInfo:      &t.srcStatusInfo,
						BaseDNSDomain:   t.dnsDomain,
						PullSecretSet:   t.pullSecretSet,
						ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
						ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
						NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
						StatusUpdatedAt: strfmt.DateTime(time.Now()),
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				if t.candidateChecker != nil {
					t.candidateChecker()
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}
			})
		}
	})
})

var _ = Describe("Refresh Cluster - Same networks", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *eventsapi.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string
		mockS3Api                               *s3wrapper.MockAPI
	)

	type candidateChecker func()

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()

		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			srcStatusInfo      string
			machineNetworks    []*models.MachineNetwork
			serviceNetworks    []*models.ServiceNetwork
			clusterNetworks    []*models.ClusterNetwork
			apiVip             string
			apiVips            []*models.APIVip
			ingressVip         string
			ingressVips        []*models.IngressVip
			dnsDomain          string
			pullSecretSet      bool
			dstState           string
			hosts              []models.Host
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			candidateChecker   candidateChecker
			errorExpected      bool
		}{
			{
				name:            "Missing service and cluster networks",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationPending, messagePattern: "At least one of the CIDRs [(]Machine Network, Cluster Network, Service Network[)] is undefined."},
				}),
				errorExpected: false,
			},
			{
				name:            "IPv6 service network",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationFailure, messagePattern: "Address families of networks .* are not the same."},
				}),
				errorExpected: false,
			},
			{
				name:            "Mixed with dual stack",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestDualStackNetworking.MachineNetworks,
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationFailure, messagePattern: "Address families of networks .* are not the same."},
				}),
				errorExpected: false,
			},
			{
				name:            "IPv4 only",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationSuccess, messagePattern: "Same address families for all networks."},
				}),
				errorExpected: false,
			},
			{
				name:            "IPv6 only",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				clusterNetworks: common.TestIPv6Networking.ClusterNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationSuccess, messagePattern: "Same address families for all networks."},
				}),
				errorExpected: false,
			},
			{
				name:            "Dual stack only",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestDualStackNetworking.MachineNetworks,
				serviceNetworks: common.TestDualStackNetworking.ServiceNetworks,
				clusterNetworks: common.TestDualStackNetworking.ClusterNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					NetworksSameAddressFamilies: {status: ValidationSuccess, messagePattern: "Same address families for all networks."},
				}),
				errorExpected: false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVips:         t.apiVips,
						ID:              &clusterId,
						IngressVips:     t.ingressVips,
						MachineNetworks: t.machineNetworks,
						Status:          &t.srcState,
						StatusInfo:      &t.srcStatusInfo,
						BaseDNSDomain:   t.dnsDomain,
						PullSecretSet:   t.pullSecretSet,
						ClusterNetworks: t.clusterNetworks,
						ServiceNetworks: t.serviceNetworks,
						NetworkType:     swag.String(models.ClusterNetworkTypeOVNKubernetes),
						StatusUpdatedAt: strfmt.DateTime(time.Now()),
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				if t.candidateChecker != nil {
					t.candidateChecker()
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}
			})
		}
	})
})

var _ = Describe("RefreshCluster - preparing for install", func() {
	var (
		ctx                         = context.Background()
		db                          *gorm.DB
		clusterId, hid1, hid2, hid3 strfmt.UUID
		cluster                     common.Cluster
		clusterApi                  *Manager
		mockEvents                  *eventsapi.MockHandler
		mockHostAPI                 *host.MockAPI
		mockMetric                  *metrics.MockAPI
		ctrl                        *gomock.Controller
		dbName                      string
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		dnsApi := dns.NewDNSHandler(nil, common.GetTestLog())
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, dnsApi, nil, nil, false)

		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	tests := []struct {
		name               string
		apiVip             string
		apiVips            []*models.APIVip
		ingressVip         string
		ingressVips        []*models.IngressVip
		dstState           string
		installationStatus string
		hosts              []models.Host
		statusInfoChecker  statusInfoChecker
		validationsChecker *validationsChecker
	}{
		{
			name:               "no change",
			apiVips:            common.TestIPv4Networking.APIVips,
			ingressVips:        common.TestIPv4Networking.IngressVips,
			dstState:           models.ClusterStatusPreparingForInstallation,
			installationStatus: models.LastInstallationPreparationStatusNotStarted,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
			},
			statusInfoChecker: makeValueChecker(statusInfoPreparingForInstallation),
		},
		{
			name:               "one insufficient host",
			apiVips:            common.TestIPv4Networking.APIVips,
			ingressVips:        common.TestIPv4Networking.IngressVips,
			dstState:           models.ClusterStatusInsufficient,
			installationStatus: models.LastInstallationPreparationStatusNotStarted,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusInsufficient),
				},
			},
			statusInfoChecker: makeValueChecker(statusInfoUnpreparingHostExists),
		},
		{
			name:        "one insufficient host + preparation failed",
			apiVips:     common.TestIPv4Networking.APIVips,
			ingressVips: common.TestIPv4Networking.IngressVips,
			dstState:    models.ClusterStatusInsufficient,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusInsufficient),
				},
			},
			installationStatus: models.LastInstallationPreparationStatusFailed,
			statusInfoChecker:  makeValueChecker(statusInfoUnpreparingHostExists),
		},
		{
			name:        "one insufficient host + preparation succeeded",
			apiVips:     common.TestIPv4Networking.APIVips,
			ingressVips: common.TestIPv4Networking.IngressVips,
			dstState:    models.ClusterStatusInsufficient,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusInsufficient),
				},
			},
			installationStatus: models.LastInstallationPreparationStatusSuccess,
			statusInfoChecker:  makeValueChecker(statusInfoUnpreparingHostExists),
		},
		{
			name:        "preparation failed",
			apiVips:     common.TestIPv4Networking.APIVips,
			ingressVips: common.TestIPv4Networking.IngressVips,
			dstState:    models.ClusterStatusReady,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusPreparingForInstallation),
				},
			},
			installationStatus: models.LastInstallationPreparationStatusFailed,
			statusInfoChecker:  makeValueChecker(statusInfoClusterFailedToPrepare),
		},
		{
			name:        "all hosts prepared + preparation succeeded",
			apiVips:     common.TestIPv4Networking.APIVips,
			ingressVips: common.TestIPv4Networking.IngressVips,
			dstState:    models.ClusterStatusInstalling,
			hosts: []models.Host{
				{
					ID:     &hid1,
					Status: swag.String(models.HostStatusPreparingSuccessful),
				},
				{
					ID:     &hid2,
					Status: swag.String(models.HostStatusPreparingSuccessful),
				},
				{
					ID:     &hid3,
					Status: swag.String(models.HostStatusPreparingSuccessful),
				},
			},
			installationStatus: models.LastInstallationPreparationStatusSuccess,
			statusInfoChecker:  makeValueChecker(statusInfoInstalling),
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster = common.Cluster{
				Cluster: models.Cluster{
					APIVips:         t.apiVips,
					ID:              &clusterId,
					IngressVips:     t.ingressVips,
					PullSecretSet:   true,
					BaseDNSDomain:   "test.com",
					Status:          swag.String(models.ClusterStatusPreparingForInstallation),
					StatusInfo:      swag.String(statusInfoPreparingForInstallation),
					StatusUpdatedAt: strfmt.DateTime(time.Now()),
					LastInstallationPreparation: models.LastInstallationPreparation{
						Status: t.installationStatus,
						Reason: "",
					},
				},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			for i := range t.hosts {
				t.hosts[i].InfraEnvID = clusterId
				t.hosts[i].ClusterID = &clusterId
				Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
			}
			cluster = getClusterFromDB(clusterId, db)
			if t.dstState != models.ClusterStatusPreparingForInstallation {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName))).AnyTimes()
			}
			switch t.dstState {
			case models.ClusterStatusInsufficient:
				mockHostAPIIsRequireUserActionResetFalse()
			case models.ClusterStatusInstalling:
				mockMetric.EXPECT().InstallationStarted().Times(1)
			}
			Expect(cluster.ValidationsInfo).To(BeEmpty())
			clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
			t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
			if t.validationsChecker != nil {
				t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
				Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
			}
		})
	}
})

var _ = Describe("Refresh Cluster - Advanced networking validations", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *eventsapi.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                  string
			srcState              string
			srcStatusInfo         string
			clusterNetworks       []*models.ClusterNetwork
			serviceNetworks       []*models.ServiceNetwork
			machineNetworks       []*models.MachineNetwork
			apiVip                string
			apiVips               []*models.APIVip
			ingressVip            string
			ingressVips           []*models.IngressVip
			dstState              string
			hosts                 []models.Host
			statusInfoChecker     statusInfoChecker
			validationsChecker    *validationsChecker
			errorExpected         bool
			userManagedNetworking bool
			vipDhcpAllocation     bool
			networkType           string
			sno                   bool
		}{
			{
				name:            "pending-for-input to pending-for-input",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Machine Network, Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
				}),
				errorExpected: false,
			},
			{
				name:        "pending-for-input to pending-for-input user-managed-networking",
				srcState:    models.ClusterStatusPendingForInput,
				dstState:    models.ClusterStatusPendingForInput,
				apiVip:      "",
				apiVips:     nil,
				ingressVip:  "",
				ingressVips: nil,
				networkType: "",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is not required: User Managed Networking"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: "The network type is not valid; the valid network types are OpenShiftSDN or OVNKubernetes"},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:            "pending-for-input to ready user-managed-networking",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is not required: User Managed Networking"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:     "pending-for-input to insufficient - overlapping (sno)", //MGMT-8748
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: models.Subnet("10.16.0.0/14")},
				},
				serviceNetworks: []*models.ServiceNetwork{
					{Cidr: models.Subnet("172.5.0.0/16")},
				},
				machineNetworks: []*models.MachineNetwork{
					{Cidr: models.Subnet("10.16.116.80/28")},
				},
				apiVip:      "10.16.116.81",
				apiVips:     []*models.APIVip{{IP: "10.16.116.81"}},
				ingressVip:  "10.16.116.82",
				ingressVips: []*models.IngressVip{{IP: "10.16.116.82"}},
				networkType: models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown),
						Inventory: common.GenerateTestInventoryWithNetwork(common.NetAddress{
							IPv4Address: []string{"10.16.116.80/28"},
						}),
						Role: models.HostRoleMaster},
				},
				sno:               true,
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ClusterNetworkCidr: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 0, must be a positive integer"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - overlapping",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv4Networking.ClusterNetworks[0].Cidr},
				}, serviceNetworks: []*models.ServiceNetwork{
					{Cidr: models.Subnet(common.IncrementCidrMask(string(common.TestIPv4Networking.MachineNetworks[0].Cidr)))},
				},
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOpenShiftSDN,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 0, must be a positive integer"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - prefix too low",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv4Networking.ClusterNetworks[0].Cidr, HostPrefix: 1},
				},
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Cluster network CIDR prefix .* does not contain enough addresses for 5 hosts each one with 1 prefix"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - prefix too high",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv4Networking.ClusterNetworks[0].Cidr, HostPrefix: common.TestIPv4Networking.ClusterNetworks[0].HostPrefix * 2},
				},
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now .*, must be less than or equal to 25 to allow at least 128 addresses"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to ready",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - not enough addresses",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv4Networking.ClusterNetworks[0].Cidr, HostPrefix: common.TestIPv4Networking.ClusterNetworks[0].HostPrefix / 2},
				},
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to ready - with workers",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv4Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv4Networking.ServiceNetworks,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:              "pending-for-input to insufficient - networkType invalid (OVN and VIP DHCP allocation)",
				srcState:          models.ClusterStatusPendingForInput,
				dstState:          models.ClusterStatusInsufficient,
				clusterNetworks:   common.TestIPv4Networking.ClusterNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				apiVips:           common.TestIPv4Networking.APIVips,
				ingressVips:       common.TestIPv4Networking.IngressVips,
				networkType:       models.ClusterNetworkTypeOVNKubernetes,
				vipDhcpAllocation: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: "VIP DHCP allocation is not supported when the cluster is configured to use OVNKubernetes."},
				}),
				errorExpected: false,
			},
			{
				name:              "pending-for-input to insufficient - networkType invalid (SNO)",
				srcState:          models.ClusterStatusPendingForInput,
				dstState:          models.ClusterStatusInsufficient,
				clusterNetworks:   common.TestIPv4Networking.ClusterNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				apiVips:           common.TestIPv4Networking.APIVips,
				ingressVips:       common.TestIPv4Networking.IngressVips,
				networkType:       models.ClusterNetworkTypeOpenShiftSDN,
				sno:               true,
				vipDhcpAllocation: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: regexp.QuoteMeta("High-availability mode 'None' (SNO) is not supported by OpenShiftSDN; use another network type instead")},
				}),
				errorExpected: false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVips:               t.apiVips,
						ID:                    &clusterId,
						IngressVips:           t.ingressVips,
						ClusterNetworks:       t.clusterNetworks,
						ServiceNetworks:       t.serviceNetworks,
						MachineNetworks:       t.machineNetworks,
						Status:                &t.srcState,
						StatusInfo:            &t.srcStatusInfo,
						PullSecretSet:         true,
						BaseDNSDomain:         "test.com",
						UserManagedNetworking: &t.userManagedNetworking,
						NetworkType:           &t.networkType,
						VipDhcpAllocation:     &t.vipDhcpAllocation,
					},
				}
				if t.sno {
					ha := models.ClusterHighAvailabilityModeNone
					cluster.HighAvailabilityMode = &ha
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}
			})
		}
	})
	Context("All transitions IPv6", func() {
		var srcState string
		tests := []struct {
			name                  string
			srcState              string
			srcStatusInfo         string
			clusterNetworks       []*models.ClusterNetwork
			serviceNetworks       []*models.ServiceNetwork
			machineNetworks       []*models.MachineNetwork
			apiVip                string
			apiVips               []*models.APIVip
			ingressVip            string
			ingressVips           []*models.IngressVip
			dstState              string
			hosts                 []models.Host
			statusInfoChecker     statusInfoChecker
			validationsChecker    *validationsChecker
			errorExpected         bool
			userManagedNetworking bool
			networkType           string
		}{
			{
				name:            "pending-for-input to pending-for-input",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusPendingForInput,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Machine Network, Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:        "pending-for-input to pending-for-input user-managed-networking",
				srcState:    models.ClusterStatusPendingForInput,
				dstState:    models.ClusterStatusPendingForInput,
				apiVip:      "",
				apiVips:     nil,
				ingressVip:  "",
				ingressVips: nil,
				networkType: "bla",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is not required: User Managed Networking"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: "The network type is not valid; the valid network types are OpenShiftSDN or OVNKubernetes"}}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:        "pending-for-input to pending-for-input user-managed-networking",
				srcState:    models.ClusterStatusPendingForInput,
				dstState:    models.ClusterStatusPendingForInput,
				apiVip:      "",
				apiVips:     nil,
				ingressVip:  "",
				ingressVips: nil,
				networkType: "bla",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is not required: User Managed Networking"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: "The network type is not valid; the valid network types are OpenShiftSDN or OVNKubernetes"}}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:            "pending-for-input to ready user-managed-networking testing_now",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv6Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is not required: User Managed Networking"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: User Managed Networking"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:     "pending-for-input to insufficient - overlapping",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv6Networking.ClusterNetworks[0].Cidr},
				},
				serviceNetworks: []*models.ServiceNetwork{
					{Cidr: models.Subnet(common.IncrementCidrMask(string(common.TestIPv6Networking.MachineNetworks[0].Cidr)))},
				},
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 0, must be a positive integer"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - prefix too low",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv6Networking.ClusterNetworks[0].Cidr, HostPrefix: 1},
				},
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Cluster network CIDR prefix .* does not contain enough addresses for 5 hosts each one with 1 prefix"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient networkType invalid",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				clusterNetworks: common.TestIPv6Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOpenShiftSDN,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},

				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationFailure, messagePattern: "The cluster is configured with IPv6 which is not supported by OpenShiftSDN; use OVNKubernetes instead"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - prefix too high",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv6Networking.ClusterNetworks[0].Cidr, HostPrefix: common.TestIPv6Networking.ClusterNetworks[0].HostPrefix * 2},
				},
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now .*, must be less than or equal to 121 to allow at least 128 addresses"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to ready",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv6Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:     "pending-for-input to insufficient - not enough addresses",
				srcState: models.ClusterStatusPendingForInput,
				dstState: models.ClusterStatusInsufficient,
				clusterNetworks: []*models.ClusterNetwork{
					{Cidr: common.TestIPv6Networking.ClusterNetworks[0].Cidr, HostPrefix: common.TestIPv6Networking.ClusterNetworks[0].HostPrefix / 2},
				},
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to ready",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				clusterNetworks: common.TestIPv6Networking.ClusterNetworks,
				serviceNetworks: common.TestIPv6Networking.ServiceNetworks,
				machineNetworks: common.TestIPv6Networking.MachineNetworks,
				apiVips:         common.TestIPv6Networking.APIVips,
				ingressVips:     common.TestIPv6Networking.IngressVips,
				networkType:     models.ClusterNetworkTypeOVNKubernetes,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
					isNetworkTypeValid:                  {status: ValidationSuccess, messagePattern: "The cluster has a valid network type"},
				}),
				errorExpected: false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVips:               t.apiVips,
						ID:                    &clusterId,
						IngressVips:           t.ingressVips,
						ClusterNetworks:       t.clusterNetworks,
						ServiceNetworks:       t.serviceNetworks,
						MachineNetworks:       t.machineNetworks,
						Status:                &t.srcState,
						StatusInfo:            &t.srcStatusInfo,
						PullSecretSet:         true,
						BaseDNSDomain:         "test.com",
						UserManagedNetworking: &t.userManagedNetworking,
						NetworkType:           &t.networkType,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}

			})
		}
	})
})

var _ = Describe("Refresh Cluster - With DHCP", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *eventsapi.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string
		mockS3Api                               *s3wrapper.MockAPI
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	clearApiVipsVerfication := func(vips []*models.APIVip) []*models.APIVip {
		return funk.Map(vips, func(v *models.APIVip) *models.APIVip {
			return &models.APIVip{
				ClusterID: v.ClusterID,
				IP:        v.IP,
			}
		}).([]*models.APIVip)
	}

	clearIngressVIpsVerification := func(vips []*models.IngressVip) []*models.IngressVip {
		return funk.Map(vips, func(v *models.IngressVip) *models.IngressVip {
			return &models.IngressVip{
				ClusterID: v.ClusterID,
				IP:        v.IP,
			}
		}).([]*models.IngressVip)
	}

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			machineNetworks         []*models.MachineNetwork
			apiVip                  string
			apiVips                 []*models.APIVip
			ingressVip              string
			ingressVips             []*models.IngressVip
			dnsDomain               string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			vipDhcpAllocation       bool
			errorExpected           bool
		}{
			{
				name:              "pending-for-input to pending-for-input",
				srcState:          models.ClusterStatusPendingForInput,
				dstState:          models.ClusterStatusPendingForInput,
				apiVip:            "",
				apiVips:           nil,
				ingressVip:        "",
				ingressVips:       nil,
				dnsDomain:         "test.com",
				pullSecretSet:     true,
				vipDhcpAllocation: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationPending, messagePattern: "Hosts have not been discovered yet"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined"},
					AreApiVipsDefined:                   {status: ValidationPending, messagePattern: "Machine Network CIDR is undefined"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationPending, messagePattern: "Machine Network CIDR is undefined"},
					AreIngressVipsValid:                 {status: ValidationPending, messagePattern: "Ingress virtual IPs are undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - masters > 3",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - not all hosts are ready to install - not enough workers",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to insufficient - not all hosts are ready to install",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInsufficient), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to insufficient - vip not matching cidr",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVip:          "10.10.10.12",
				apiVips:         []*models.APIVip{{IP: "10.10.10.12"}},
				ingressVip:      "10.10.10.13",
				ingressVips:     []*models.IngressVip{{IP: "10.10.10.13"}},
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:      {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					AreApiVipsDefined:         {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:           {status: ValidationFailure, messagePattern: fmt.Sprintf("api vips <10.10.10.12> does not belong to machine-network-cidr <%s>", string(common.TestIPv4Networking.MachineNetworks[0].Cidr))},
					AreIngressVipsDefined:     {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:       {status: ValidationFailure, messagePattern: fmt.Sprintf("ingress vips <10.10.10.13> does not belong to machine-network-cidr <%s>", string(common.TestIPv4Networking.MachineNetworks[0].Cidr))},
					AllHostsAreReadyToInstall: {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install."},
					IsDNSDomainDefined:        {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:           {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:    {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to insufficient - vip is taken",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         clearApiVipsVerfication(common.TestIPv4Networking.APIVips),
				ingressVips:     clearIngressVIpsVerification(common.TestIPv4Networking.IngressVips),
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster, FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses("1.2.3.0/24", "1.2.3.9", "1.2.3.8"))},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster, FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses("1.2.3.0/24", "1.2.3.9", "1.2.3.8"))},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster, FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses("1.2.3.0/24", "1.2.3.9", "1.2.3.8"))},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker, FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses("1.2.3.0/24", "1.2.3.9", "1.2.3.8"))},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker, FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses("1.2.3.0/24", "1.2.3.59", "1.2.3.8"))},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationFailure, messagePattern: fmt.Sprintf("api vips <%s> is already in use in cidr %s", common.TestIPv4Networking.APIVips[0].IP, string(common.TestIPv4Networking.MachineNetworks[0].Cidr))},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationFailure, messagePattern: fmt.Sprintf("ingress vips <%s> is already in use in cidr %s", common.TestIPv4Networking.IngressVips[0].IP, string(common.TestIPv4Networking.MachineNetworks[0].Cidr))},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},

			{
				name:              "ready to dhcp timeout - api vip not defined",
				srcState:          models.ClusterStatusReady,
				dstState:          models.ClusterStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				apiVip:            "",
				apiVips:           nil,
				ingressVips:       common.TestIPv4Networking.IngressVips,
				dnsDomain:         "test.com",
				pullSecretSet:     true,
				vipDhcpAllocation: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined; IP allocation from the DHCP server timed out"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:              "ready to insufficient - api vip not defined",
				srcState:          models.ClusterStatusReady,
				dstState:          models.ClusterStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				apiVip:            "",
				apiVips:           nil,
				ingressVips:       common.TestIPv4Networking.IngressVips,
				dnsDomain:         "test.com",
				pullSecretSet:     true,
				vipDhcpAllocation: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationFailure, messagePattern: "API virtual IPs are undefined; after the Machine Network CIDR has been defined, API virtual IPs are received from a DHCP lease allocation task which may take up to 2 minutes"},
					AreApiVipsValid:                     {status: ValidationPending, messagePattern: "API virtual IPs are undefined"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				setMachineCidrUpdatedAt: true,
				errorExpected:           false,
			},
			{
				name:            "dhcp timeout to ready",
				srcState:        models.ClusterStatusInsufficient,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "ingress vips .* belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "pending-for-input to ready",
				srcState:        models.ClusterStatusPendingForInput,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "insufficient to ready",
				srcState:        models.ClusterStatusInsufficient,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "ready to ready",
				srcState:        models.ClusterStatusReady,
				dstState:        models.ClusterStatusReady,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:            "finalizing to finalizing",
				srcState:        models.ClusterStatusFinalizing,
				srcStatusInfo:   statusInfoFinalizing,
				dstState:        models.ClusterStatusFinalizing,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoFinalizing),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "error to error",
				srcState:        models.ClusterStatusError,
				dstState:        models.ClusterStatusError,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "installed to installed",
				srcState:        models.ClusterStatusInstalled,
				srcStatusInfo:   statusInfoInstalled,
				dstState:        models.ClusterStatusInstalled,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoInstalled),
				validationsChecker: nil,
				errorExpected:      false,
			},
			{
				name:            "Installing to Installing",
				srcState:        models.ClusterStatusInstalling,
				srcStatusInfo:   statusInfoInstalling,
				dstState:        models.ClusterStatusInstalling,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				apiVips:         common.TestIPv4Networking.APIVips,
				ingressVips:     common.TestIPv4Networking.IngressVips,
				dnsDomain:       "test.com",
				pullSecretSet:   true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoInstalling),
				validationsChecker: nil,
				errorExpected:      false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVips:           t.apiVips,
						ID:                &clusterId,
						IngressVips:       t.ingressVips,
						MachineNetworks:   t.machineNetworks,
						Status:            &t.srcState,
						StatusInfo:        &t.srcStatusInfo,
						VipDhcpAllocation: &t.vipDhcpAllocation,
						BaseDNSDomain:     t.dnsDomain,
						PullSecretSet:     t.pullSecretSet,
						ClusterNetworks:   common.TestIPv4Networking.ClusterNetworks,
						ServiceNetworks:   common.TestIPv4Networking.ServiceNetworks,
						NetworkType:       swag.String(models.ClusterNetworkTypeOVNKubernetes),
						StatusUpdatedAt:   strfmt.DateTime(time.Now()),
					},
				}
				if t.setMachineCidrUpdatedAt {
					cluster.MachineNetworkCidrUpdatedAt = time.Now()
				} else {
					cluster.MachineNetworkCidrUpdatedAt = time.Now().Add(-3 * time.Minute)
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}

				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}
			})
		}
	})
})

var _ = Describe("Refresh Cluster - Installing Cases", func() {
	var (
		ctx                                           = context.Background()
		db                                            *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5, hid6 strfmt.UUID
		cluster                                       common.Cluster
		clusterApi                                    *Manager
		mockEvents                                    *eventsapi.MockHandler
		mockHostAPI                                   *host.MockAPI
		mockMetric                                    *metrics.MockAPI
		mockS3Api                                     *s3wrapper.MockAPI
		mockAccountsMgmt                              *ocm.MockOCMAccountsMgmt
		operatorsManager                              *operators.Manager
		ctrl                                          *gomock.Controller
		dbName                                        string
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	mockMetricsAPIInstallationFinished := func() {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		operatorsManager = operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		hid6 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                string
			srcState            string
			srcStatusInfo       string
			pullSecretSet       bool
			dstState            string
			hosts               []models.Host
			statusInfoChecker   statusInfoChecker
			withOCMClient       bool
			requiresAMSUpdate   bool
			installationTimeout bool
			vipDhcpAllocation   bool
			operators           []*models.MonitoredOperator
		}{
			{
				name:          "installing to installing",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusInstalling,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.ClusterStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.ClusterStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.ClusterStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.ClusterStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.ClusterStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstalling),
			},
			{
				name:          "installing to installing-pending-user-action",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusInstallingPendingUserAction,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstallingPendingUserAction),
			},
			{
				name:          "installing to installing-pending-user-action (2)",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusInstallingPendingUserAction,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstallingPendingUserAction),
			},
			{
				name:          "finalizing to installing-pending-user-action",
				srcState:      models.ClusterStatusFinalizing,
				srcStatusInfo: statusInfoFinalizing,
				dstState:      models.ClusterStatusInstallingPendingUserAction,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid6, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstallingPendingUserAction),
			},
			{
				name:          "installing-pending-user-action to installing-pending-user-action",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusInstallingPendingUserAction,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstallingPendingUserAction),
			},
			{
				name:          "installing-pending-user-action to installing-pending-user-action (2)",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusInstallingPendingUserAction,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstallingPendingUserAction),
			},
			{
				name:          "installing-pending-user-action to error",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusError), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoError),
			},
			{
				name:          "installing-pending-user-action to error (2)",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusError), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoError),
			},
			{
				name:          "installing-pending-user-action to error due to timeout",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstallingPendingUserAction), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:   makeValueChecker(statusInfoTimeout),
				installationTimeout: true,
			},
			{
				name:          "installing-pending-user-action to installing",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusInstalling,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstalling),
			},
			{
				name:          "installing-pending-user-action to finalizing",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusFinalizing,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid6, Status: swag.String(models.HostStatusInstallingInProgress), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoFinalizing),
			},
			{
				name:          "installing-pending-user-action to finalizing (2)",
				srcState:      models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo: statusInfoInstallingPendingUserAction,
				dstState:      models.ClusterStatusFinalizing,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid6, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoFinalizing),
			},
			{
				name:          "installing to finalizing",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusFinalizing,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoFinalizing),
			},
			{
				name:          "installing to error - failing master",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusError), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoError),
			},
			{
				name:          "installing to error - failing worker, a single worker isn't supported",
				srcState:      models.ClusterStatusInstalling,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusError), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoError),
			},
			{
				name:          "finalizing to error due to timeout",
				srcState:      models.ClusterStatusFinalizing,
				srcStatusInfo: statusInfoFinalizingTimeout,
				dstState:      models.ClusterStatusError,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:   makeValueChecker(statusInfoFinalizingTimeout),
				installationTimeout: true,
			},
			{
				name:          "finalizing to finalizing",
				srcState:      models.ClusterStatusFinalizing,
				srcStatusInfo: statusInfoFinalizing,
				dstState:      models.ClusterStatusFinalizing,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoFinalizing),
				withOCMClient:     true,
				requiresAMSUpdate: true,
				operators: []*models.MonitoredOperator{
					{
						Name:         operators.OperatorConsole.Name,
						OperatorType: models.OperatorTypeBuiltin,
						Status:       models.OperatorStatusAvailable,
					},
				},
			},
			{
				name:          "finalizing to finalizing (2)",
				srcState:      models.ClusterStatusFinalizing,
				srcStatusInfo: statusInfoFinalizing,
				dstState:      models.ClusterStatusFinalizing,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoFinalizing),
				withOCMClient:     true,
				// console URL should be updated only once in AMS
				requiresAMSUpdate: false,
				operators: []*models.MonitoredOperator{
					{
						Name:         operators.OperatorConsole.Name,
						OperatorType: models.OperatorTypeBuiltin,
						Status:       models.OperatorStatusAvailable,
					},
				},
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						ClusterNetworks:    common.TestIPv4Networking.ClusterNetworks,
						ServiceNetworks:    common.TestIPv4Networking.ServiceNetworks,
						MachineNetworks:    common.TestIPv4Networking.MachineNetworks,
						APIVips:            common.TestIPv4Networking.APIVips,
						IngressVips:        common.TestIPv4Networking.IngressVips,
						ID:                 &clusterId,
						Status:             &t.srcState,
						StatusInfo:         &t.srcStatusInfo,
						VipDhcpAllocation:  &t.vipDhcpAllocation,
						BaseDNSDomain:      "test.com",
						PullSecretSet:      t.pullSecretSet,
						MonitoredOperators: t.operators,
						StatusUpdatedAt:    strfmt.DateTime(time.Now()),
					},
				}
				if t.withOCMClient {
					mockAccountsMgmt = ocm.NewMockOCMAccountsMgmt(ctrl)
					ocmClient := &ocm.Client{AccountsMgmt: mockAccountsMgmt, Config: &ocm.Config{}}
					clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
						mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, ocmClient, mockS3Api, nil, nil, nil, false)
					if !t.requiresAMSUpdate {
						cluster.IsAmsSubscriptionConsoleUrlSet = true
					}
				}
				cluster.MachineNetworkCidrUpdatedAt = time.Now().Add(-3 * time.Minute)
				if t.installationTimeout && t.srcState != models.ClusterStatusFinalizing {
					// adjust the cluster InstallStartedAt to trigger a timeout
					cluster.InstallStartedAt = strfmt.DateTime(time.Now().Add(-25 * time.Hour))
				} else if t.installationTimeout && t.srcState == models.ClusterStatusFinalizing {
					// adjust the cluster StatusUpdatedAt to trigger a timeout
					cluster.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-6 * time.Hour))
				} else {
					cluster.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Hour))
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				if t.srcState == models.ClusterStatusFinalizing && !t.requiresAMSUpdate && !t.installationTimeout &&
					funk.ContainsString([]string{models.ClusterStatusInstalled, models.ClusterStatusFinalizing}, t.dstState) {
					mockS3Api.EXPECT().DoesObjectExist(ctx, fmt.Sprintf("%s/%s", cluster.ID, constants.Kubeconfig)).Return(false, nil)
				}
				reportInstallationCompleteStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError, models.ClusterStatusInstallingPendingUserAction}
				if funk.Contains(reportInstallationCompleteStatuses, t.dstState) && t.srcState != t.dstState && t.srcState != models.ClusterStatusInstallingPendingUserAction {
					mockMetricsAPIInstallationFinished()
				} else if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				if t.requiresAMSUpdate {
					subscriptionID := cluster.AmsSubscriptionID
					consoleUrl := common.GetConsoleUrl(cluster.Name, cluster.BaseDNSDomain)
					mockAccountsMgmt.EXPECT().UpdateSubscriptionConsoleUrl(ctx, subscriptionID, consoleUrl)
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				Expect(clusterAfterRefresh).ToNot(BeNil())
				Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
			})
		}
	})
})

var _ = Describe("Log Collection - refresh cluster", func() {
	var (
		ctx         = context.Background()
		db          *gorm.DB
		clusterId   strfmt.UUID
		cluster     common.Cluster
		clusterApi  *Manager
		mockEvents  *eventsapi.MockHandler
		mockHostAPI *host.MockAPI
		mockMetric  *metrics.MockAPI
		ctrl        *gomock.Controller
		dbName      string
	)

	var (
		StatusUpdatedAt           strfmt.DateTime
		ControllerLogsCollectedAt strfmt.DateTime
		ControllerLogsStartedAt   strfmt.DateTime
		srclogsInfo               models.LogsState
		dstlogsInfo               models.LogsState
		srcState                  string
		srcStatusInfo             string
	)

	logTimeoutConfig := func() Config {
		cfg := getDefaultConfig()
		cfg.PrepareConfig.LogCollectionTimeout = 1 * time.Second
		cfg.PrepareConfig.LogPendingTimeout = 1 * time.Second
		return cfg
	}

	verifyStatusNotChanged := func(c *common.Cluster, srcState string, srcStatusInfo string) {
		Expect(c.Status).To(Equal(&srcState))
		Expect(c.StatusInfo).To(Equal(&srcStatusInfo))
		Expect(c.ValidationsInfo).To(BeEmpty())
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		clusterApi = NewManager(logTimeoutConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID:                        &clusterId,
				Status:                    &srcState,
				StatusInfo:                &srcStatusInfo,
				StatusUpdatedAt:           StatusUpdatedAt,
				LogsInfo:                  srclogsInfo,
				ControllerLogsCollectedAt: ControllerLogsCollectedAt,
				ControllerLogsStartedAt:   ControllerLogsStartedAt,
			},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = getClusterFromDB(clusterId, db)
		clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
		Expect(err).ToNot(HaveOccurred())
		verifyStatusNotChanged(clusterAfterRefresh, srcState, srcStatusInfo)
		Expect(clusterAfterRefresh.LogsInfo).To(Equal(dstlogsInfo))

		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("refresh on error state", func() {

		BeforeEach(func() {
			srcState = models.ClusterStatusError
			srcStatusInfo = statusInfoError
		})

		It("logs not requested when cluster enter error -> mark as timeout to signal that we do not wait for them", func() {
			srclogsInfo = ""
			dstlogsInfo = "timeout"
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Time{})
			ControllerLogsStartedAt = strfmt.DateTime(time.Time{})
		})

		It("logs requested when cluster enter error -> timeout", func() {
			srclogsInfo = models.LogsStateRequested
			dstlogsInfo = models.LogsStateTimeout
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Time{})
			ControllerLogsStartedAt = strfmt.DateTime(time.Time{})
		})

		It("logs requested when cluster enter error -> no timeout", func() {
			srclogsInfo = models.LogsStateRequested
			dstlogsInfo = models.LogsStateRequested
			StatusUpdatedAt = strfmt.DateTime(time.Now())
			ControllerLogsCollectedAt = strfmt.DateTime(time.Time{})
			ControllerLogsStartedAt = strfmt.DateTime(time.Now())
		})

		It("logs collected in the past but not completed -> timeout", func() {
			srclogsInfo = models.LogsStateCollecting
			dstlogsInfo = models.LogsStateTimeout
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsStartedAt = strfmt.DateTime(time.Now().Add(-3 * time.Second))
		})

		It("logs collected in the past and then re-requested but not collected again -> timeout", func() {
			srclogsInfo = models.LogsStateRequested
			dstlogsInfo = models.LogsStateTimeout
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Now().Add(-3 * time.Second))
			ControllerLogsStartedAt = strfmt.DateTime(time.Now())
		})

		It("logs collected in the past and then re-requested within timeout limits -> no timeout", func() {
			srclogsInfo = models.LogsStateRequested
			dstlogsInfo = models.LogsStateRequested
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Now().Add(-500 * time.Millisecond))
			ControllerLogsStartedAt = strfmt.DateTime(time.Now())
		})

		It("logs completed -> no timeout", func() {
			srclogsInfo = models.LogsStateCompleted
			dstlogsInfo = models.LogsStateCompleted
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsStartedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
		})
	})

	Context("refresh on cancel state", func() {

		BeforeEach(func() {
			srcState = models.ClusterStatusCancelled
			srcStatusInfo = "cancelled"
		})

		It("logs not requested when cluster enter cancel -> mark as timeout to signal that we do not wait for them", func() {
			srclogsInfo = ""
			dstlogsInfo = models.LogsStateTimeout
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Time{})
			ControllerLogsStartedAt = strfmt.DateTime(time.Time{})
		})

		It("logs requested when cluster enter cancel -> timeout", func() {
			srclogsInfo = models.LogsStateRequested
			dstlogsInfo = models.LogsStateTimeout
			StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-2 * time.Second))
			ControllerLogsCollectedAt = strfmt.DateTime(time.Time{})
			ControllerLogsStartedAt = strfmt.DateTime(time.Time{})
		})
	})
})

var _ = Describe("NTP refresh cluster", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *eventsapi.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil, false)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:          "pending-for-input to insufficient - ntp problem",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239 - 400, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationFailure, messagePattern: "please configure an NTP server via DHCP"},
				}),
				errorExpected: false,
			},
			{
				name:          "pending-for-input to ready",
				srcState:      models.ClusterStatusPendingForInput,
				dstState:      models.ClusterStatusReady,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:          "insufficient to ready",
				srcState:      models.ClusterStatusInsufficient,
				dstState:      models.ClusterStatusReady,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:          "ready to ready",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusReady,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:          "ready to insufficient with disconnected",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventory(), Timestamp: 1601909239 + 1000, Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventory(), Timestamp: 1601909239 - 1000, Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:          "ready to insufficient with needs o be rebooted status",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventory(), Timestamp: 1601909239 + 1000, Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventory(), Timestamp: 1601909239 - 1000, Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined."},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR."},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined."},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined."},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined."},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:          "ready to insufficient",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239 - 400, Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are defined"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
					IsNtpServerConfigured:               {status: ValidationFailure, messagePattern: "please configure an NTP server via DHCP"},
				}),
				errorExpected: false,
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						ClusterNetworks: common.TestIPv4Networking.ClusterNetworks,
						ServiceNetworks: common.TestIPv4Networking.ServiceNetworks,
						MachineNetworks: common.TestIPv4Networking.MachineNetworks,
						APIVips:         common.TestIPv4Networking.APIVips,
						IngressVips:     common.TestIPv4Networking.IngressVips,
						ID:              &clusterId,
						Status:          &t.srcState,
						StatusInfo:      &t.srcStatusInfo,
						BaseDNSDomain:   "test.com",
						PullSecretSet:   t.pullSecretSet,

						NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
				}
				mockHostAPIIsRequireUserActionResetFalse()
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				Expect(cluster.ValidationsInfo).To(BeEmpty())
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
					Expect(clusterAfterRefresh.ValidationsInfo).ToNot(BeEmpty())
				} else {
					Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
				}
			})
		}
	})
})

var _ = Describe("Single node", func() {
	var (
		ctx                         = context.Background()
		db                          *gorm.DB
		clusterId, hid1, hid2, hid3 strfmt.UUID
		cluster                     common.Cluster
		clusterApi                  *Manager
		mockEvents                  *eventsapi.MockHandler
		mockHostAPI                 *host.MockAPI
		mockMetric                  *metrics.MockAPI
		ctrl                        *gomock.Controller
		dbName                      string
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	mockIsValidMasterCandidate := func() {
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		dnsApi := dns.NewDNSHandler(nil, common.GetTestLog())
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, dnsApi, nil, nil, false)
		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("single node transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:          "non ha mode, too many nodes",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single control plane node and no workers."},
				}),
				errorExpected: false,
			},
			{
				name:          "non ha mode, 2 nodes, master and worker",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single control plane node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:          "non ha mode, 3 nodes, master and 2 workers",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleWorker},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single control plane node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:          "non ha mode, 1 worker, no masters",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusInsufficient,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single control plane node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:          "non ha mode, 1 node master",
				srcState:      models.ClusterStatusReady,
				dstState:      models.ClusterStatusReady,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},

			{
				name:          "non ha mode, 1 node auto assign",
				srcState:      models.ClusterStatusInsufficient,
				dstState:      models.ClusterStatusReady,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleAutoAssign},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
			{
				name:          "set API and Ingress vip for SNO",
				srcState:      models.ClusterStatusPreparingForInstallation,
				srcStatusInfo: statusInfoInstalling,
				dstState:      models.ClusterStatusInstalling,
				pullSecretSet: true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusPreparingSuccessful), Inventory: defaultInventory(), Timestamp: 1601909239, Role: models.HostRoleMaster, Bootstrap: true},
				},
				statusInfoChecker: makeValueChecker(statusInfoInstalling),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					AreApiVipsDefined:                   {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreApiVipsValid:                     {status: ValidationSuccess, messagePattern: "API virtual IPs are not required: SNO"},
					AreIngressVipsDefined:               {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AreIngressVipsValid:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IPs are not required: SNO"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				}),
				errorExpected: false,
			},
		}
		for i := range tests {
			t := tests[i]
			haMode := models.ClusterHighAvailabilityModeNone
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						ClusterNetworks:      common.TestIPv4Networking.ClusterNetworks,
						ServiceNetworks:      common.TestIPv4Networking.ServiceNetworks,
						MachineNetworks:      common.TestIPv4Networking.MachineNetworks,
						APIVips:              common.TestIPv4Networking.APIVips,
						IngressVips:          common.TestIPv4Networking.IngressVips,
						ID:                   &clusterId,
						Status:               &t.srcState,
						StatusInfo:           &t.srcStatusInfo,
						BaseDNSDomain:        "test.com",
						PullSecretSet:        t.pullSecretSet,
						NetworkType:          swag.String(models.ClusterNetworkTypeOVNKubernetes),
						HighAvailabilityMode: &haMode,
					},
				}
				if t.srcState == models.ClusterStatusPreparingForInstallation && t.dstState == models.ClusterStatusInstalling {
					cluster.Cluster.StatusUpdatedAt = strfmt.DateTime(time.Now())
					cluster.LastInstallationPreparation = models.LastInstallationPreparation{
						Status: models.LastInstallationPreparationStatusSuccess,
						Reason: "",
					}

					mockMetric.EXPECT().InstallationStarted().Times(1)

				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				mockIsValidMasterCandidate()
				for i := range t.hosts {
					t.hosts[i].InfraEnvID = clusterId
					t.hosts[i].ClusterID = &clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
						eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()

				}
				mockHostAPIIsRequireUserActionResetFalse()

				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
				}
			})
		}
	})
})

var _ = Describe("ValidationResult sort", func() {
	It("ValidationResult sort", func() {
		validationResults := []ValidationResult{
			{ID: "cab", Status: "abc", Message: "abc"},
			{ID: "bac", Status: "abc", Message: "abc"},
			{ID: "acb", Status: "abc", Message: "abc"},
			{ID: "abc", Status: "abc", Message: "abc"},
		}
		sortByValidationResultID(validationResults)
		Expect(validationResults[0].ID.String()).Should(Equal("abc"))
		Expect(validationResults[1].ID.String()).Should(Equal("acb"))
		Expect(validationResults[2].ID.String()).Should(Equal("bac"))
		Expect(validationResults[3].ID.String()).Should(Equal("cab"))
	})
})

var _ = Describe("finalizing timeouts", func() {
	var (
		ctx              = context.Background()
		db               *gorm.DB
		clusterId        strfmt.UUID
		clusterApi       *Manager
		mockEvents       *eventsapi.MockHandler
		mockHostAPI      *host.MockAPI
		mockMetric       *metrics.MockAPI
		ctrl             *gomock.Controller
		dbName           string
		operatorsManager *operators.Manager
		mockS3Api        *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		operatorsManager = operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
	createCluster := func(status string, stage models.FinalizingStage, statusTimestamp, stageTimestamp time.Time) *common.Cluster {
		cls := &common.Cluster{
			Cluster: models.Cluster{
				ID:               &clusterId,
				Status:           swag.String(status),
				StatusInfo:       swag.String(statusInfoFinalizing),
				StatusUpdatedAt:  strfmt.DateTime(statusTimestamp),
				OpenshiftVersion: "4.15",
				EmailDomain:      "redhat.com",
				Progress: &models.ClusterProgressInfo{
					FinalizingStage:          stage,
					FinalizingStageStartedAt: strfmt.DateTime(stageTimestamp),
				},
				OrgSoftTimeoutsEnabled: true,
			},
		}
		Expect(db.Create(cls).Error).ToNot(HaveOccurred())
		return cls
	}
	testUnexpiredTimeout := func(stage models.FinalizingStage, timeout time.Duration) {
		cls := createCluster(models.ClusterStatusFinalizing, stage, time.Now(), time.Now().Add(-(timeout - time.Second)))
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
		clusterAfterUpdate, err := clusterApi.RefreshStatus(ctx, cls, db)
		Expect(err).ToNot(HaveOccurred())
		Expect(swag.StringValue(clusterAfterUpdate.Status)).To(Equal(models.ClusterStatusFinalizing))
		Expect(swag.StringValue(clusterAfterUpdate.StatusInfo)).To(Equal(statusInfoFinalizing))
	}
	Context("soft timeouts disabled", func() {
		BeforeEach(func() {
			clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
				mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, false)
		})
		for _, st := range finalizingStages {
			stage := st
			timeout := finalizingStageTimeout(stage, nil, logrus.New())
			It(fmt.Sprintf("finalizing stage '%s' timeout expired", stage), func() {
				cls := createCluster(models.ClusterStatusFinalizing, stage, time.Now(), time.Now().Add(-(timeout + time.Second)))
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).Times(1)
				mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusError, models.ClusterStatusFinalizing, cls.OpenshiftVersion, *cls.ID, cls.EmailDomain, gomock.Any())
				clusterAfterUpdate, err := clusterApi.RefreshStatus(ctx, cls, db)
				Expect(err).ToNot(HaveOccurred())
				Expect(swag.StringValue(clusterAfterUpdate.Status)).To(Equal(models.ClusterStatusError))
				Expect(swag.StringValue(clusterAfterUpdate.StatusInfo)).To(Equal(fmt.Sprintf(statusInfoFinalizingStageTimeout, stage, int64(timeout.Minutes()))))
			})
			It(fmt.Sprintf("finalizing stage '%s' timeout didn't expire", stage), func() {
				testUnexpiredTimeout(stage, timeout)
			})
		}
	})
	Context("soft timeouts enabled", func() {
		BeforeEach(func() {
			clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db, testing.GetDummyNotificationStream(ctrl),
				mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api, nil, nil, nil, true)

		})
		It("finalizing status timeout not active", func() {
			cls := createCluster(models.ClusterStatusFinalizing, "", time.Now().Add(-(clusterApi.Config.FinalizingTimeout + time.Second)), time.Now())
			mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
			clusterAfterUpdate, err := clusterApi.RefreshStatus(ctx, cls, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(swag.StringValue(clusterAfterUpdate.Status)).To(Equal(models.ClusterStatusFinalizing))
		})
		for _, st := range finalizingStages {
			stage := st
			timeout := finalizingStageTimeout(stage, nil, logrus.New())
			It(fmt.Sprintf("finalizing stage '%s' timeout expired", stage), func() {
				cls := createCluster(models.ClusterStatusFinalizing, stage, time.Now(), time.Now().Add(-(timeout + time.Second)))
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).Times(1)
				clusterAfterUpdate, err := clusterApi.RefreshStatus(ctx, cls, db)
				Expect(err).ToNot(HaveOccurred())
				Expect(swag.StringValue(clusterAfterUpdate.Status)).To(Equal(models.ClusterStatusFinalizing))
				Expect(swag.StringValue(clusterAfterUpdate.StatusInfo)).To(Equal(fmt.Sprintf(statusInfoFinalizingStageSoftTimeout, stage, int64(timeout.Minutes()))))
				Expect(clusterAfterUpdate.Progress.FinalizingStageTimedOut).To(BeTrue())
			})
			It(fmt.Sprintf("finalizing stage '%s' timeout didn't expire", stage), func() {
				testUnexpiredTimeout(stage, timeout)
			})
		}
	})
})

func getClusterFromDB(clusterId strfmt.UUID, db *gorm.DB) common.Cluster {
	c, err := common.GetClusterFromDB(db, clusterId, common.UseEagerLoading)
	ExpectWithOffset(1, err).ShouldNot(HaveOccurred())
	return *c
}

func makeFreeAddresses(network string, ips ...strfmt.IPv4) *models.FreeNetworkAddresses {
	return &models.FreeNetworkAddresses{
		FreeAddresses: ips,
		Network:       network,
	}
}

func makeFreeNetworksAddressesStr(elems ...*models.FreeNetworkAddresses) string {
	toMarshal := models.FreeNetworksAddresses(elems)
	b, err := json.Marshal(&toMarshal)
	Expect(err).ToNot(HaveOccurred())
	return string(b)
}
