package host

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("ReportValidationFailedMetrics", func() {
	var (
		ctx                 = context.Background()
		ctrl                *gomock.Controller
		mockMetrics         *metrics.MockAPI
		hostMetricsReporter HostMetricsReporterAPI
		mockEvents          *eventsapi.MockHandler
		hostID              = strfmt.UUID(uuid.New().String())
		clusterID           = strfmt.UUID(uuid.New().String())
	)

	toJsonString := func(results ValidationsStatus) string {
		validationInfoBytes, _ := json.Marshal(results)
		return string(validationInfoBytes)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetrics = metrics.NewMockAPI(ctrl)

		hostMetricsReporter = NewHostMetricsReporter(common.GetTestLog(), mockMetrics, nil)
	})

	Context("when empty validation info", func() {
		It("should not call metrics API, but not fail", func() {
			mockMetrics.EXPECT().HostValidationFailed(gomock.Any()).Times(0)
			h := &models.Host{
				ID:              &hostID,
				ClusterID:       &clusterID,
				ValidationsInfo: "",
			}
			err := hostMetricsReporter.ReportValidationFailedMetrics(
				ctx,
				h,
			)
			Expect(err).Should(BeNil())
		})
	})

	Context("when invalid validation info", func() {
		It("should not call metrics API and fail", func() {
			mockMetrics.EXPECT().HostValidationFailed(gomock.Any()).Times(0)
			h := &models.Host{
				ID:              &hostID,
				ClusterID:       &clusterID,
				ValidationsInfo: "not-a-valid-json",
			}
			err := hostMetricsReporter.ReportValidationFailedMetrics(
				ctx,
				h,
			)
			Expect(err).NotTo(BeNil())
		})
	})

	DescribeTable("should call metrics API given validation failure",
		func(validationsInfo string, expectedValidationErrors []models.HostValidationID) {
			for _, expectedValidationError := range expectedValidationErrors {
				mockMetrics.EXPECT().HostValidationFailed(expectedValidationError).Times(1)
			}
			h := &models.Host{
				ID:              &hostID,
				ClusterID:       &clusterID,
				ValidationsInfo: validationsInfo,
			}
			err := hostMetricsReporter.ReportValidationFailedMetrics(
				ctx,
				h,
			)
			Expect(err).Should(BeNil())
		},
		Entry(
			"Single validation failure",
			toJsonString(ValidationsStatus{
				"hw": {
					{
						ID:     HasMinCPUCores,
						Status: ValidationFailure,
					},
				},
			}),
			[]models.HostValidationID{models.HostValidationIDHasMinCPUCores},
		),
		Entry(
			"Multiple validation failures of the same type",
			toJsonString(ValidationsStatus{
				"hw": {
					{
						ID:     HasMinCPUCores,
						Status: ValidationSuccess,
					},
					{
						ID:     HasMinValidDisks,
						Status: ValidationFailure,
					},
				},
			}),
			[]models.HostValidationID{models.HostValidationIDHasMinValidDisks},
		),
	)

	DescribeTable("should NOT call metrics API given validation failure",
		func(validationsInfo string) {
			mockMetrics.EXPECT().HostValidationFailed(gomock.Any()).Times(0)
			h := &models.Host{
				ID:              &hostID,
				ClusterID:       &clusterID,
				ValidationsInfo: validationsInfo,
			}
			err := hostMetricsReporter.ReportValidationFailedMetrics(
				ctx,
				h,
			)
			Expect(err).Should(BeNil())
		},
		Entry(
			"Single validation success",
			toJsonString(ValidationsStatus{
				"hw": {
					{
						ID:     HasMinCPUCores,
						Status: ValidationSuccess,
					},
				},
			}),
		),
		Entry(
			"Multiple successess with the same key",
			toJsonString(ValidationsStatus{
				"hw": {
					{
						ID:     HasMinCPUCores,
						Status: ValidationSuccess,
					},
					{
						ID:     HasMinValidDisks,
						Status: ValidationSuccess,
					},
				},
			}),
		),
		Entry(
			"Multiple successes with different keys",
			toJsonString(ValidationsStatus{
				"hw": {
					{
						ID:     HasMinCPUCores,
						Status: ValidationSuccess,
					},
					{
						ID:     HasMinValidDisks,
						Status: ValidationSuccess,
					},
				},
				"foo": {
					{
						ID:     CompatibleAgent,
						Status: ValidationSuccess,
					},
				},
			}),
		),
	)
})

var _ = Describe("ReportHostInstallationmetrics", func() {
	var (
		boot                *models.Disk
		ctx                 = context.Background()
		ctrl                *gomock.Controller
		mockMetrics         *metrics.MockAPI
		host                *models.Host
		mockEvents          *eventsapi.MockHandler
		hostMetricsReporter HostMetricsReporterAPI
		hostID              = strfmt.UUID(uuid.New().String())
		clusterID           = strfmt.UUID(uuid.New().String())
	)

	toJsonString := func(results ValidationsStatus) string {
		validationInfoBytes, _ := json.Marshal(results)
		return string(validationInfoBytes)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		host = &models.Host{
			ID: &hostID,
		}
		boot = &models.Disk{}
		mockMetrics = metrics.NewMockAPI(ctrl)

		hostMetricsReporter = NewHostMetricsReporter(common.GetTestLog(), mockMetrics, nil)
	})

	DescribeTable("When current stage is not different from previous stage, then it should do nothing",
		func(previousProgress *models.HostProgressInfo, currentStage models.HostStage) {
			hostMetricsReporter.ReportHostInstallationMetrics(
				ctx,
				clusterID,
				boot,
				host,
				previousProgress,
				currentStage,
			)
		},
		Entry(
			"previous progress nil",
			nil,
			models.HostStage{},
		),
		Entry(
			"previous progress is the same as current stage",
			&models.HostProgressInfo{
				CurrentStage: models.HostStageRebooting,
			},
			models.HostStageRebooting,
		),
	)
})
