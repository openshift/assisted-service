package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

var _ = Describe("Transition tests", func() {
	var (
		ctx              = context.Background()
		capi             API
		db               *gorm.DB
		clusterId        strfmt.UUID
		eventsHandler    events.Handler
		ctrl             *gomock.Controller
		mockMetric       *metrics.MockAPI
		dbName           = "cluster_transition_test"
		operatorsManager *operators.MockAPI
		mockS3Api        *s3wrapper.MockAPI
		mockAccountsMgmt *ocm.MockOCMAccountsMgmt
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		eventsHandler = events.New(db, logrus.New())
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockAccountsMgmt = ocm.NewMockOCMAccountsMgmt(ctrl)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("cancel_installation", func() {
		BeforeEach(func() {
			capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, mockMetric, nil, nil, operatorsManager, nil, nil)
		})

		It("cancel_installation", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
			Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
		})

		It("cancel_installation_conflict", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusInsufficient)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
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
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
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
			dstState                     string
		}{
			{
				name:     "no change",
				dstState: models.ClusterStatusFinalizing,
			},
			// TODO: MGMT-4458
			// Backward-compatible solution for clusters that don't have monitored operators data
			// Those clusters shouldn't finish until the controller would tell them.
			{
				name:                       "success - no operators (backward-compatability)",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				dstState:                   models.ClusterStatusInstalled,
			},
			{
				name:                       "success - available operators",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusAvailable,
					},
				},
				dstState: models.ClusterStatusInstalled,
			},
			{
				name:                       "no change - failed operator",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: false,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeBuiltin,
						Status: models.OperatorStatusFailed,
					},
				},

				dstState: models.ClusterStatusFinalizing,
			},
			{
				name:                       "success - failed OLM operator",
				uploadKubeConfig:           true,
				updateSuccessfullyFinished: true,
				operators: []*models.MonitoredOperator{
					{
						Name: common.TestDefaultConfig.MonitoredOperator.Name, OperatorType: models.OperatorTypeOlm,
						Status: models.OperatorStatusFailed,
					},
				},
				dstState: models.ClusterStatusInstalled,
			},
			{
				name:                         "success - with AMS",
				uploadKubeConfig:             true,
				updateSuccessfullyFinished:   true,
				updateAMSSubscription:        true,
				updateAMSSubscriptionSuccess: true,
				dstState:                     models.ClusterStatusInstalled,
			},
			{
				name:                         "success - with AMS (update failed) -> finalizing",
				uploadKubeConfig:             true,
				updateSuccessfullyFinished:   true,
				updateAMSSubscription:        true,
				updateAMSSubscriptionSuccess: false,
				errorExpected:                true,
				dstState:                     models.ClusterStatusFinalizing,
			},
		}

		checkCompleteInstallationUpdate := func(eventSeverity string, eventMessage string) {
			events, err := eventsHandler.GetEvents(clusterId, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
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
					},
				}
				Expect(common.LoadTableFromDB(db, common.MonitoredOperatorsTable).Create(&c).Error).ShouldNot(HaveOccurred())

				var ocmClient *ocm.Client = nil

				if t.updateAMSSubscription {
					ocmClient = &ocm.Client{AccountsMgmt: mockAccountsMgmt, Config: &ocm.Config{WithAMSSubscriptions: true}}
				}

				if t.updateSuccessfullyFinished {
					if t.updateAMSSubscription && t.updateAMSSubscriptionSuccess {
						mockAccountsMgmt.EXPECT().UpdateSubscriptionPostInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
					} else {
						mockAccountsMgmt.EXPECT().UpdateSubscriptionPostInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("error")).Times(1)
					}
				}

				mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(t.uploadKubeConfig, nil).AnyTimes() // Might be affected by the amount of states

				if t.dstState != *c.Status {
					if !t.errorExpected {
						mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), t.dstState, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt).Times(1)
					}
				}

				capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, eventsHandler, nil, mockMetric, nil, nil, operatorsManager, ocmClient, mockS3Api)

				// Test
				clusterAfterRefresh, err := capi.RefreshStatus(ctx, &c, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))

					if t.dstState != *c.Status {
						if t.updateSuccessfullyFinished {
							checkCompleteInstallationUpdate(models.EventSeverityInfo, "Successfully finished installing cluster")
						} else {
							checkCompleteInstallationUpdate(models.EventSeverityCritical, fmt.Sprintf("Operator %s failed", t.operators[0].Name))
						}
					}
				}

				clusterFromDB := getClusterFromDB(clusterId, db)
				Expect(clusterFromDB.Status).To(Equal(&t.dstState))
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Cancel cluster installation", func() {
	var (
		ctx               = context.Background()
		dbName            = "cancel_cluster_installation_test"
		capi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
		mockMetric        *metrics.MockAPI
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEventsHandler, nil, mockMetric, nil, nil, operatorsManager, nil, nil)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	acceptClusterInstallationFinished := func(times int) {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true},
		{state: models.ClusterStatusInstalling, success: true},
		{state: models.ClusterStatusError, success: true},
		{state: models.ClusterStatusFinalizing, success: true},
		{state: models.ClusterStatusInstallingPendingUserAction, success: true},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict},
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.success {
				acceptClusterInstallationFinished(1)
			}
			acceptNewEvents(eventsNum)
			err := capi.CancelInstallation(ctx, &cluster, "reason", db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
			}
		})
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Reset cluster", func() {
	var (
		ctx               = context.Background()
		dbName            = "reset_cluster_test"
		capi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		capi = NewManager(getDefaultConfig(), common.GetTestLog(), db, mockEventsHandler, nil, nil, nil, nil, operatorsManager, nil, nil)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true},
		{state: models.ClusterStatusInstalling, success: true},
		{state: models.ClusterStatusError, success: true},
		{state: models.ClusterStatusFinalizing, success: true},
		{state: models.ClusterStatusInstallingPendingUserAction, success: true},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict},
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("reset from state %s", t.state), func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			acceptNewEvents(eventsNum)
			err := capi.ResetCluster(ctx, &cluster, "reason", db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
			}
		})
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
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
	validationMap := make(map[string][]ValidationResult)
	Expect(json.Unmarshal([]byte(validationsStr), &validationMap)).ToNot(HaveOccurred())
next:
	for id, checkedResult := range j.expected {
		category, err := id.Category()
		Expect(err).ToNot(HaveOccurred())
		results, ok := validationMap[category]
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
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string = "cluster_transition_test_refresh_host_no_dhcp"
		mockS3Api                               *s3wrapper.MockAPI
	)

	type candidateChecker func()

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	checkMasterCandidates := func(times int) candidateChecker {
		return func() {
			mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(times)
		}
	}

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			srcStatusInfo      string
			machineNetworkCidr string
			apiVip             string
			ingressVip         string
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
				name:               "pending-for-input to pending-for-input",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined"},
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "API virtual IP is undefined"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationFailure, messagePattern: "Ingress virtual IP is undefined"},
					IsIngressVipValid:                   {status: ValidationPending, messagePattern: "Ingress virtual IP is undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to pending-for-input with 2 workers",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "The Machine Network CIDR is undefined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined."},
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "The API virtual IP is undefined"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "The API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationFailure, messagePattern: "The Ingress virtual IP is undefined"},
					IsIngressVipValid:                   {status: ValidationPending, messagePattern: "The Ingress virtual IP is undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationSuccess,
						messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to pending-for-input with 3 master 2 workers candidates in auto-assign mode",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "The Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined."},
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "The API virtual IP is undefined"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "The API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationFailure, messagePattern: "The Ingress virtual IP is undefined"},
					IsIngressVipValid:                   {status: ValidationPending, messagePattern: "The Ingress virtual IP is undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationSuccess,
						messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - masters > 3",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - worker = 1 with auto-assign",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleAutoAssign},
				},
				candidateChecker:  checkMasterCandidates(3),
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - worker = 1",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - not all hosts are ready to install",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to pending-for-input - api vip not defined",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "API virtual IP is undefined"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to pending-for-input - dns domain not defined",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationFailure, messagePattern: "The base domain is undefined and must be provided"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to pending-for-input - pull secret not set",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      false,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationFailure, messagePattern: "The pull secret is not set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			}, {
				name:               "pending-for-input to ready",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to ready with disabled master",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusDisabled), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to ready",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to ready",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "finalizing to finalizing",
				srcState:           models.ClusterStatusFinalizing,
				srcStatusInfo:      statusInfoFinalizing,
				dstState:           models.ClusterStatusFinalizing,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
				name:               "error to error",
				srcState:           models.ClusterStatusError,
				dstState:           models.ClusterStatusError,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
				name:               "installed to installed",
				srcState:           models.ClusterStatusInstalled,
				srcStatusInfo:      statusInfoInstalled,
				dstState:           models.ClusterStatusInstalled,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
				name:               "Installing to Installing",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusInstalling,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ClusterNetworkCidr:       "1.3.0.0/16",
						ServiceNetworkCidr:       "1.4.0.0/16",
						ClusterNetworkHostPrefix: 24,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				}
				if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				if t.candidateChecker != nil {
					t.candidateChecker()
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
				} else {
					//refresh status in installing calls to PostRefreshCluster and update the validation info
					if t.dstState == models.ClusterStatusInstalling {
						Expect(clusterAfterRefresh.ValidationsInfo).To(Equal("{}"))
					} else {
						//refresh status in PreparingForInstallation, Finalizing, Installed, Error and AddingHosts will not update the validation info
						Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
					}
				}
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("Refresh Cluster - Advanced networking validations", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string = "cluster_transition_test_refresh_host_no_dhcp"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                     string
			srcState                 string
			srcStatusInfo            string
			machineNetworkCidr       string
			clusterNetworkCidr       string
			serviceNetworkCidr       string
			clusterNetworkHostPrefix int64
			apiVip                   string
			ingressVip               string
			dstState                 string
			hosts                    []models.Host
			statusInfoChecker        statusInfoChecker
			validationsChecker       *validationsChecker
			errorExpected            bool
			userManagedNetworking    bool
		}{
			{
				name:               "pending-for-input to pending-for-input",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.4",
				ingressVip:         "1.2.3.5",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Machine Network, Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to pending-for-input user-managed-networking",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:                     "pending-for-input to ready user-managed-networking",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.8.0/23",
				clusterNetworkCidr:       "1.3.0.0/21",
				clusterNetworkHostPrefix: 23,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:               "pending-for-input to insufficient - overlapping # 1",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.4",
				ingressVip:         "1.2.3.5",
				serviceNetworkCidr: "1.2.2.0/23",
				clusterNetworkCidr: "1.2.2.0/24",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 0, must be a positive integer"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient - overlapping # 2",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.2.0/23",
				clusterNetworkCidr:       "1.2.2.0/24",
				clusterNetworkHostPrefix: 1,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Cluster network CIDR prefix 24 does not contain enough addresses for 5 hosts each one with 1 prefix"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient - prefix too high",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.2.0/23",
				clusterNetworkCidr:       "1.2.2.0/24",
				clusterNetworkHostPrefix: 26,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 26, must be less than or equal to 25 to allow at least 128 addresses"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to ready",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.8.0/23",
				clusterNetworkCidr:       "1.3.0.0/21",
				clusterNetworkHostPrefix: 23,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient (1)",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.8.0/23",
				clusterNetworkCidr:       "1.3.0.0/22",
				clusterNetworkHostPrefix: 23,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient (2)",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.8.0/23",
				clusterNetworkCidr:       "1.3.0.0/22",
				clusterNetworkHostPrefix: 24,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient (3)",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1.2.3.0/24",
				apiVip:                   "1.2.3.4",
				ingressVip:               "1.2.3.5",
				serviceNetworkCidr:       "1.2.8.0/23",
				clusterNetworkCidr:       "1.3.0.0/21",
				clusterNetworkHostPrefix: 24,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
				}),
				errorExpected: false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						ClusterNetworkCidr:       t.clusterNetworkCidr,
						ServiceNetworkCidr:       t.serviceNetworkCidr,
						ClusterNetworkHostPrefix: t.clusterNetworkHostPrefix,
						PullSecretSet:            true,
						BaseDNSDomain:            "test.com",
						UserManagedNetworking:    &t.userManagedNetworking,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				}
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
	Context("All transitions IPv6", func() {
		var srcState string
		tests := []struct {
			name                     string
			srcState                 string
			srcStatusInfo            string
			machineNetworkCidr       string
			clusterNetworkCidr       string
			serviceNetworkCidr       string
			clusterNetworkHostPrefix int64
			apiVip                   string
			ingressVip               string
			dstState                 string
			hosts                    []models.Host
			statusInfoChecker        statusInfoChecker
			validationsChecker       *validationsChecker
			errorExpected            bool
			userManagedNetworking    bool
		}{
			{
				name:               "pending-for-input to pending-for-input",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1001:db8::/120",
				apiVip:             "1001:db8::64",
				ingressVip:         "1001:db8::65",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Machine Network, Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to pending-for-input user-managed-networking",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationFailure, messagePattern: "Cluster Network CIDR is undefined"},
					isServiceCidrDefined:                {status: ValidationFailure, messagePattern: "Service Network CIDR is undefined"},
					noCidrOverlapping:                   {status: ValidationPending, messagePattern: "At least one of the CIDRs .Cluster Network, Service Network. is undefined"},
					networkPrefixValid:                  {status: ValidationPending, messagePattern: "Cluster Network CIDR is undefined"},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:                     "pending-for-input to ready user-managed-networking",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1002:db8::/119",
				clusterNetworkCidr:       "1003:db8::/53",
				clusterNetworkHostPrefix: 64,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "The API virtual IP is not required: User Managed Networking"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is not required: User Managed Networking"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:               "pending-for-input to insufficient - overlapping # 1",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1001:db8::/120",
				apiVip:             "1001:db8::64",
				ingressVip:         "1001:db8::65",
				serviceNetworkCidr: "1001:db8::/119",
				clusterNetworkCidr: "1001:db8::/118",
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 0, must be a positive integer"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient - overlapping # 2",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1001:db8::/119",
				clusterNetworkCidr:       "1001:db8::/118",
				clusterNetworkHostPrefix: 1,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Cluster network CIDR prefix 118 does not contain enough addresses for 5 hosts each one with 1 prefix"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient - prefix too high",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1001:db8::/119",
				clusterNetworkCidr:       "1001:db8::/118",
				clusterNetworkHostPrefix: 122,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationFailure, messagePattern: "MachineNetworkCIDR and ServiceNetworkCIDR: CIDRS .* and .* overlap"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "Host prefix, now 122, must be less than or equal to 121 to allow at least 128 addresses"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to ready",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1002:db8::/119",
				clusterNetworkCidr:       "1003:db8::/53",
				clusterNetworkHostPrefix: 64,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient (1)",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1002:db8::/119",
				clusterNetworkCidr:       "1003:db8::/52",
				clusterNetworkHostPrefix: 53,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to insufficient (2)",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusInsufficient,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1002:db8::/119",
				clusterNetworkCidr:       "1003:db8::/52",
				clusterNetworkHostPrefix: 54,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationFailure, messagePattern: "does not contain enough addresses for"},
				}),
				errorExpected: false,
			},
			{
				name:                     "pending-for-input to ready",
				srcState:                 models.ClusterStatusPendingForInput,
				dstState:                 models.ClusterStatusReady,
				machineNetworkCidr:       "1001:db8::/120",
				apiVip:                   "1001:db8::64",
				ingressVip:               "1001:db8::65",
				serviceNetworkCidr:       "1002:db8::/119",
				clusterNetworkCidr:       "1003:db8::/53",
				clusterNetworkHostPrefix: 65,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					isClusterCidrDefined:                {status: ValidationSuccess, messagePattern: "Cluster Network CIDR is defined"},
					isServiceCidrDefined:                {status: ValidationSuccess, messagePattern: "Service Network CIDR is defined"},
					noCidrOverlapping:                   {status: ValidationSuccess, messagePattern: "No CIDRS are overlapping"},
					networkPrefixValid:                  {status: ValidationSuccess, messagePattern: "Cluster Network prefix is valid."},
				}),
				errorExpected: false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						ClusterNetworkCidr:       t.clusterNetworkCidr,
						ServiceNetworkCidr:       t.serviceNetworkCidr,
						ClusterNetworkHostPrefix: t.clusterNetworkHostPrefix,
						PullSecretSet:            true,
						BaseDNSDomain:            "test.com",
						UserManagedNetworking:    &t.userManagedNetworking,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				}
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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("Refresh Cluster - With DHCP", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string = "cluster_transition_test_refresh_host_with_dhcp"
		mockS3Api                               *s3wrapper.MockAPI
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		mockS3Api = s3wrapper.NewMockAPI(ctrl)
		mockS3Api.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, mockS3Api)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			machineNetworkCidr      string
			apiVip                  string
			ingressVip              string
			dnsDomain               string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:               "pending-for-input to pending-for-input",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined"},
					IsApiVipDefined:                     {status: ValidationPending, messagePattern: "Machine Network CIDR is undefined"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationPending, messagePattern: "Machine Network CIDR is undefined"},
					IsIngressVipValid:                   {status: ValidationPending, messagePattern: "Ingress virtual IP is undefined"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - masters > 3",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount: {status: ValidationFailure,
						messagePattern: fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported",
							common.MinMasterHostsNeededForInstallation)},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - not all hosts are ready to install - not enough workers",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Clusters with less than 3 dedicated masters or a single worker"},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to insufficient - not all hosts are ready to install",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to dhcp timeout - api vip not defined",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "API virtual IP is undefined; IP allocation from the DHCP server timed out"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to insufficient - api vip not defined",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationFailure, messagePattern: "API virtual IP is undefined; after the Machine Network CIDR has been defined, the API virtual IP is received from a DHCP lease allocation task which may take up to 2 minutes"},
					IsApiVipValid:                       {status: ValidationPending, messagePattern: "API virtual IP is undefined"},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				setMachineCidrUpdatedAt: true,
				errorExpected:           false,
			},
			{
				name:               "dhcp timeout to ready",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.7",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "ingress vip 1.2.3.6 belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to ready",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to ready",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to ready",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},
			{
				name:               "finalizing to finalizing",
				srcState:           models.ClusterStatusFinalizing,
				srcStatusInfo:      statusInfoFinalizing,
				dstState:           models.ClusterStatusFinalizing,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
				name:               "error to error",
				srcState:           models.ClusterStatusError,
				dstState:           models.ClusterStatusError,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
				name:               "installed to installed",
				srcState:           models.ClusterStatusInstalled,
				srcStatusInfo:      statusInfoInstalled,
				dstState:           models.ClusterStatusInstalled,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "Installing to Installing",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusInstalling,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
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
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						VipDhcpAllocation:        swag.Bool(true),
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ServiceNetworkCidr:       "1.2.4.0/24",
						ClusterNetworkCidr:       "1.3.0.0/16",
						ClusterNetworkHostPrefix: 24,
					},
				}
				if t.setMachineCidrUpdatedAt {
					cluster.MachineNetworkCidrUpdatedAt = time.Now()
				} else {
					cluster.MachineNetworkCidrUpdatedAt = time.Now().Add(-3 * time.Minute)
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				}
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
				} else {
					//refresh status in installing calls to PostRefreshCluster and update the validation info
					if t.dstState == models.ClusterStatusInstalling {
						Expect(clusterAfterRefresh.ValidationsInfo).To(Equal("{}"))
					} else {
						//refresh status in PreparingForInstallation, Finalizing, Installed, Error and AddingHosts will not update the validation info
						Expect(clusterAfterRefresh.ValidationsInfo).To(BeEmpty())
					}
				}
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("Refresh Cluster - Installing Cases", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  = "cluster_transition_test_refresh_installing_cases"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	mockMetricsAPIInstallationFinished := func() {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			srcStatusInfo      string
			machineNetworkCidr string
			apiVip             string
			ingressVip         string
			dnsDomain          string
			pullSecretSet      bool
			dstState           string
			hosts              []models.Host
			statusInfoChecker  statusInfoChecker
		}{
			{
				name:               "installing to installing",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusInstalling,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing to installing-pending-user-action",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusInstallingPendingUserAction,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing to installing-pending-user-action (2)",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusInstallingPendingUserAction,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing-pending-user-action to installing-pending-user-action",
				srcState:           models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo:      statusInfoInstallingPendingUserAction,
				dstState:           models.ClusterStatusInstallingPendingUserAction,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing-pending-user-action to installing-pending-user-action (2)",
				srcState:           models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo:      statusInfoInstallingPendingUserAction,
				dstState:           models.ClusterStatusInstallingPendingUserAction,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing-pending-user-action to error",
				srcState:           models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo:      statusInfoInstallingPendingUserAction,
				dstState:           models.ClusterStatusError,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing-pending-user-action to error (2)",
				srcState:           models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo:      statusInfoInstallingPendingUserAction,
				dstState:           models.ClusterStatusError,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing-pending-user-action to installing",
				srcState:           models.ClusterStatusInstallingPendingUserAction,
				srcStatusInfo:      statusInfoInstallingPendingUserAction,
				dstState:           models.ClusterStatusInstalling,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing to finalizing",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusFinalizing,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
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
				name:               "installing to error",
				srcState:           models.ClusterStatusInstalling,
				srcStatusInfo:      statusInfoInstalling,
				dstState:           models.ClusterStatusError,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusError), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusInstalling), Inventory: common.GenerateTestDefaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoError),
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						VipDhcpAllocation:        swag.Bool(true),
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ServiceNetworkCidr:       "1.2.4.0/24",
						ClusterNetworkCidr:       "1.3.0.0/16",
						ClusterNetworkHostPrefix: 24,
					},
				}
				cluster.MachineNetworkCidrUpdatedAt = time.Now().Add(-3 * time.Minute)
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
				}
				reportInstallationCompleteStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError}
				if funk.Contains(reportInstallationCompleteStatuses, t.dstState) {
					mockMetricsAPIInstallationFinished()
				} else if t.dstState == models.ClusterStatusInsufficient {
					mockHostAPIIsRequireUserActionResetFalse()
				}
				clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("Log Collection - refresh cluster", func() {
	var (
		ctx         = context.Background()
		db          *gorm.DB
		clusterId   strfmt.UUID
		cluster     common.Cluster
		clusterApi  *Manager
		mockEvents  *events.MockHandler
		mockHostAPI *host.MockAPI
		mockMetric  *metrics.MockAPI
		ctrl        *gomock.Controller
		dbName      string = "log_refresh_cluster"
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
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(logTimeoutConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
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
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("NTP refresh cluster", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string = "cluster_transition_test_refresh_cluster_with_ntp"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)
		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			machineNetworkCidr      string
			apiVip                  string
			ingressVip              string
			dnsDomain               string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:               "pending-for-input to insufficient - ntp problem",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239 - 400), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationFailure, messagePattern: "please configure an NTP server via DHCP"},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to ready",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to ready",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to ready",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to ready with disabled",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusDisabled), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusDisabled), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient with disconnected",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient with needs o be rebooted status",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined."},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR."},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined."},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined."},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined."},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239 - 400), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
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
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ClusterNetworkCidr:       "1.3.0.0/16",
						ServiceNetworkCidr:       "1.4.0.0/16",
						ClusterNetworkHostPrefix: 24,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("NTP refresh cluster", func() {
	var (
		ctx                                     = context.Background()
		db                                      *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5 strfmt.UUID
		cluster                                 common.Cluster
		clusterApi                              *Manager
		mockEvents                              *events.MockHandler
		mockHostAPI                             *host.MockAPI
		mockMetric                              *metrics.MockAPI
		ctrl                                    *gomock.Controller
		dbName                                  string = "cluster_transition_test_refresh_cluster_with_ntp"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
		hid5 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			machineNetworkCidr      string
			apiVip                  string
			ingressVip              string
			dnsDomain               string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:               "pending-for-input to insufficient - ntp problem",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239 - 400), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationFailure, messagePattern: "please configure an NTP server via DHCP"},
				}),
				errorExpected: false,
			},
			{
				name:               "pending-for-input to ready",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to ready",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},
			{
				name:               "ready to ready",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to ready with disabled",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusDisabled), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusDisabled), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient with disconnected",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusDisconnected), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient with needs o be rebooted status",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventoryWithTimestamp(1601909239 + 1000), Role: models.HostRoleWorker},
					{ID: &hid5, Status: swag.String(models.HostStatusResettingPendingUserAction), Inventory: defaultInventoryWithTimestamp(1601909239 - 1000), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined."},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR."},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined."},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined."},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "The cluster has hosts that are not ready to install."},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined."},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set."},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
					IsNtpServerConfigured:               {status: ValidationSuccess, messagePattern: "No ntp problems found"},
				}),
				errorExpected: false,
			},

			{
				name:               "ready to insufficient",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239 - 400), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates"},
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
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ClusterNetworkCidr:       "1.3.0.0/16",
						ServiceNetworkCidr:       "1.4.0.0/16",
						ClusterNetworkHostPrefix: 24,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("Single node", func() {
	var (
		ctx                         = context.Background()
		db                          *gorm.DB
		clusterId, hid1, hid2, hid3 strfmt.UUID
		cluster                     common.Cluster
		clusterApi                  *Manager
		mockEvents                  *events.MockHandler
		mockHostAPI                 *host.MockAPI
		mockMetric                  *metrics.MockAPI
		ctrl                        *gomock.Controller
		dbName                      string = "cluster_transition_test_refresh_cluster_with_ntp"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	mockIsValidMasterCandidate := func() {
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{})
		clusterApi = NewManager(getDefaultConfig(), common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil)
		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("single node transitions", func() {
		var srcState string
		tests := []struct {
			name                    string
			srcState                string
			srcStatusInfo           string
			machineNetworkCidr      string
			apiVip                  string
			ingressVip              string
			dnsDomain               string
			pullSecretSet           bool
			dstState                string
			hosts                   []models.Host
			statusInfoChecker       statusInfoChecker
			validationsChecker      *validationsChecker
			setMachineCidrUpdatedAt bool
			errorExpected           bool
		}{
			{
				name:               "non ha mode, too much nodes",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single master node and no workers."},
				}),
				errorExpected: false,
			},
			{
				name:               "non ha mode, 2 nodes, one disabled",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusDisabled), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},

			{
				name:               "non ha mode, 2 nodes, master and worker",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single master node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:               "non ha mode, 3 nodes, master and 2 workers",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleWorker},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single master node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:               "non ha mode, 1 worker, no masters",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(StatusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationFailure, messagePattern: "Single-node clusters must have a single master node and no workers."},
				}),
				errorExpected: false,
			},

			{
				name:               "non ha mode, 1 node master",
				srcState:           models.ClusterStatusReady,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				}),
				errorExpected: false,
			},

			{
				name:               "non ha mode, 1 node auto assign",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusReady,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				dnsDomain:          "test.com",
				pullSecretSet:      true,
				hosts: []models.Host{
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventoryWithTimestamp(1601909239), Role: models.HostRoleAutoAssign},
				},
				statusInfoChecker: makeValueChecker(StatusInfoReady),
				validationsChecker: makeJsonChecker(map[ValidationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
					IsMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
					IsApiVipDefined:                     {status: ValidationSuccess, messagePattern: "The API virtual IP is defined"},
					IsApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					IsIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
					IsIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					IsDNSDomainDefined:                  {status: ValidationSuccess, messagePattern: "The base domain is defined"},
					IsPullSecretSet:                     {status: ValidationSuccess, messagePattern: "The pull secret is set"},
					SufficientMastersCount:              {status: ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
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
						APIVip:                   t.apiVip,
						ID:                       &clusterId,
						IngressVip:               t.ingressVip,
						MachineNetworkCidr:       t.machineNetworkCidr,
						Status:                   &t.srcState,
						StatusInfo:               &t.srcStatusInfo,
						BaseDNSDomain:            t.dnsDomain,
						PullSecretSet:            t.pullSecretSet,
						ClusterNetworkCidr:       "1.3.0.0/16",
						ServiceNetworkCidr:       "1.4.0.0/16",
						ClusterNetworkHostPrefix: 24,
						HighAvailabilityMode:     &haMode,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				mockIsValidMasterCandidate()
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getClusterFromDB(clusterId, db)
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

func getClusterFromDB(clusterId strfmt.UUID, db *gorm.DB) common.Cluster {
	c, err := common.GetClusterFromDB(db, clusterId, common.UseEagerLoading)
	Expect(err).ShouldNot(HaveOccurred())
	return *c
}
