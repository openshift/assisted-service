package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/golang/mock/gomock"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/sirupsen/logrus"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Transition tests", func() {
	var (
		ctx           = context.Background()
		capi          API
		db            *gorm.DB
		clusterId     strfmt.UUID
		eventsHandler events.Handler
		ctrl          *gomock.Controller
		mockMetric    *metrics.MockAPI
		dbName        = "cluster_transition_test"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		capi = NewManager(defaultTestConfig, getTestLog(), db, eventsHandler, nil, mockMetric)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, c.InstallStartedAt)
			Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
		})

		It("cancel_installation_conflict", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInsufficient)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, c.InstallStartedAt)
			replay := capi.CancelInstallation(ctx, &c, "", db)
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInsufficient))
		})

		It("cancel_failed_installation", func() {
			c := common.Cluster{
				Cluster: models.Cluster{
					ID:         &clusterId,
					StatusInfo: swag.String("original error"),
					Status:     swag.String(clusterStatusError)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, c.InstallStartedAt)
			Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
			Expect(swag.StringValue(c.StatusInfo)).Should(Equal("original error"))
		})
	})
	Context("cancel_installation", func() {
		It("complete installation success", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusFinalizing)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusInstalled, c.OpenshiftVersion, c.InstallStartedAt)
			Expect(capi.CompleteInstallation(ctx, &c, true, clusterStatusInstalled)).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalled))
		})

		It("complete installation failed", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusFinalizing)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusError, c.OpenshiftVersion, c.InstallStartedAt)
			Expect(capi.CompleteInstallation(ctx, &c, false, "aaaa")).ShouldNot(HaveOccurred())

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
			Expect(swag.StringValue(c.StatusInfo)).Should(Equal("aaaa"))

		})

		It("complete_installation_conflict", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusInstalled, c.OpenshiftVersion, c.InstallStartedAt)
			replay := capi.CompleteInstallation(ctx, &c, true, "")
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalling))
		})

		It("complete_installation_conflict_failed", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusError, c.OpenshiftVersion, c.InstallStartedAt)
			replay := capi.CompleteInstallation(ctx, &c, false, "")
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))

			Expect(db.First(&c, "id = ?", c.ID).Error).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalling))
		})
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
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		capi = NewManager(defaultTestConfig, getTestLog(), db, mockEventsHandler, nil, mockMetric)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	acceptClusterInstallationFinished := func(times int) {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true},
		{state: models.ClusterStatusInstalling, success: true},
		{state: models.ClusterStatusError, success: true},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusFinalizing, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict},
	}

	for _, t := range tests {
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.success {
				eventsNum++
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
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		capi = NewManager(defaultTestConfig, getTestLog(), db, mockEventsHandler, nil, nil)
	})

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.ClusterStatusPreparingForInstallation, success: true},
		{state: models.ClusterStatusInstalling, success: true},
		{state: models.ClusterStatusError, success: true},
		{state: models.ClusterStatusInsufficient, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusReady, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusFinalizing, success: false, statusCode: http.StatusConflict},
		{state: models.ClusterStatusInstalled, success: false, statusCode: http.StatusConflict},
	}

	for _, t := range tests {
		It(fmt.Sprintf("reset from state %s", t.state), func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.state)},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.success {
				eventsNum++
			}
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
	expected map[validationID]validationCheckResult
}

func (j *validationsChecker) check(validationsStr string) {
	validationMap := make(map[string][]validationResult)
	Expect(json.Unmarshal([]byte(validationsStr), &validationMap)).ToNot(HaveOccurred())
next:
	for id, checkedResult := range j.expected {
		category, err := id.category()
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
	status         validationStatus
	messagePattern string
}

func makeJsonChecker(expected map[validationID]validationCheckResult) *validationsChecker {
	return &validationsChecker{expected: expected}
}

var _ = Describe("Refresh Cluster", func() {
	var (
		ctx                               = context.Background()
		db                                *gorm.DB
		clusterId, hid1, hid2, hid3, hid4 strfmt.UUID
		cluster                           common.Cluster
		clusterApi                        *Manager
		mockEvents                        *events.MockHandler
		mockHostAPI                       *host.MockAPI
		mockMetric                        *metrics.MockAPI
		ctrl                              *gomock.Controller
		dbName                            string = "cluster_transition_test_refresh_host"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric)

		hid1 = strfmt.UUID(uuid.New().String())
		hid2 = strfmt.UUID(uuid.New().String())
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID(uuid.New().String())
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
			dstState           string
			hosts              []models.Host
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			errorExpected      bool
		}{
			{
				name:               "pending-for-input to pending-for-input",
				srcState:           models.ClusterStatusPendingForInput,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "",
				apiVip:             "",
				ingressVip:         "",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					isApiVipDefined:                     {status: ValidationFailure, messagePattern: "API vip is undefined"},
					isApiVipValid:                       {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					isIngressVipDefined:                 {status: ValidationFailure, messagePattern: "Ingress vip is undefined"},
					isIngressVipValid:                   {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationFailure, messagePattern: "cluster is expected to have exactly 3 known master to be installed"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
				},
				statusInfoChecker: makeValueChecker(statusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationFailure, messagePattern: "cluster is expected to have exactly 3 known master to be installed"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusInsufficient), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoInsufficient),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "Cluster has hosts that are not ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
				}),
				errorExpected: false,
			},
			{
				name:               "insufficient to pending-for-input - host in pending-for-input",
				srcState:           models.ClusterStatusInsufficient,
				dstState:           models.ClusterStatusPendingForInput,
				machineNetworkCidr: "1.2.3.0/24",
				apiVip:             "1.2.3.5",
				ingressVip:         "1.2.3.6",
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusPendingForInput), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationFailure, messagePattern: "Cluster has pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationFailure, messagePattern: "Cluster has hosts that are not ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					isApiVipDefined:                     {status: ValidationFailure, messagePattern: "API vip is undefined"},
					isApiVipValid:                       {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationPending, messagePattern: "Machine network CIDR or API vip or Ingress vip is undefined"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoReady),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoReady),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker: makeValueChecker(statusInfoReady),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsMachineCidrDefined:                {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					isMachineCidrEqualsToCalculatedCidr: {status: ValidationSuccess, messagePattern: "Cluster machine CIDR equals to the calculated CIDR"},
					isApiVipDefined:                     {status: ValidationSuccess, messagePattern: "API vip is defined"},
					isApiVipValid:                       {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					isIngressVipDefined:                 {status: ValidationSuccess, messagePattern: "Ingress vip is defined"},
					isIngressVipValid:                   {status: ValidationSuccess, messagePattern: "belongs to machine CIDR and not in use"},
					NoPendingForInputHost:               {status: ValidationSuccess, messagePattern: "Cluster does not have pending for input hosts"},
					AllHostsAreReadyToInstall:           {status: ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
					HasExactlyThreeMasters:              {status: ValidationSuccess, messagePattern: "Cluster has 3 known master hosts"},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
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
				hosts: []models.Host{
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
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
					{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleMaster},
					{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: defaultInventory(), Role: models.HostRoleWorker},
				},
				statusInfoChecker:  makeValueChecker(statusInfoInstalled),
				validationsChecker: nil,
				errorExpected:      false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = common.Cluster{
					Cluster: models.Cluster{
						APIVip:             t.apiVip,
						ID:                 &clusterId,
						IngressVip:         t.ingressVip,
						MachineNetworkCidr: t.machineNetworkCidr,
						Status:             &t.srcState,
						StatusInfo:         &t.srcStatusInfo,
					},
				}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				for i := range t.hosts {
					t.hosts[i].ClusterID = clusterId
					Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
				}
				cluster = getCluster(clusterId, db)
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

func getCluster(clusterId strfmt.UUID, db *gorm.DB) common.Cluster {
	var cluster common.Cluster
	Expect(db.Preload("Hosts").First(&cluster, "id = ?", clusterId).Error).ShouldNot(HaveOccurred())
	return cluster
}
