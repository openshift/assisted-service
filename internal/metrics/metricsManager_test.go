package metrics

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Disable selected metric events", func() {

	var (
		ctx     context.Context
		server  *MetricsServer
		ctrl    *gomock.Controller
		handler *eventsapi.MockHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		server = NewMetricsServer()
		ctrl = gomock.NewController(GinkgoT())
		handler = eventsapi.NewMockHandler(ctrl)
	})

	AfterEach(func() {
		server.Close()
		ctrl.Finish()
	})

	var disabledMetricsTest = func(disabledMetrics []string) {
		manager := NewMetricsManager(server.Registry(), handler, disabledMetrics)
		manager.InstallerReleaseCache(
			ctx,
			strfmt.UUID("68bc0049-1226-4132-acb1-b9898065474b"),
			"4.21.x86_64",
			time.Now(),
			false,
			10,
		)
		manager.ClusterInstallationFinished(
			ctx,
			"Test",
			"Testing",
			"123",
			strfmt.UUID("68bc0049-1226-4132-acb1-b9898065474b"),
			"",
			strfmt.DateTime(time.Now()),
		)
	}

	var expectMetricEvent = func(eventName string) {
		handler.EXPECT().V2AddMetricsEvent(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			eventName,
			gomock.Any(),
			gomock.Any(),
		).AnyTimes()
	}

	It("Should send a metric event if the event is not disabled", func() {
		expectMetricEvent(MetricEventInstallerCacheReleaseMetrics)
		expectMetricEvent(MetricEventClusterInstallationResults)
		disabledMetricsTest(nil)
	})

	It("Should not send a metric event if the event is disabled", func() {
		expectMetricEvent(MetricEventClusterInstallationResults)
		disabledMetricsTest([]string{MetricEventInstallerCacheReleaseMetrics})
	})
})

var _ = DescribeTable(
	"Tests that label has been added as expected",
	func(stage models.HostStage, expectedMetricRegex string) {
		// Create a context:
		ctx := context.Background()

		// Create the metrics server:
		server := NewMetricsServer()
		defer server.Close()

		// Create the mock controller:
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()

		// We are not testing generation of events here, we just need to make sure that the
		// call to generating metrics doesn't fail:
		handler := eventsapi.NewMockHandler(ctrl)
		handler.EXPECT().V2AddMetricsEvent(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).AnyTimes()

		// Create the metrics manager:
		manager := NewMetricsManager(server.Registry(), handler, nil)
		manager.ReportHostInstallationMetrics(
			ctx,
			"4.10.18",
			strfmt.UUID("68bc0049-1226-4132-acb1-b9898065474b"),
			"example.com",
			nil,
			&models.Host{
				Role: models.HostRoleMaster,
				Inventory: `{
					"system_vendor": {
						"manufacturer": "Acme",
						"product_name": "Rocket"
					}
				}`,
			},
			&models.HostProgressInfo{
				CurrentStage:   stage,
				StageStartedAt: strfmt.DateTime(time.Now()),
			},
			models.HostStageRebooting,
		)

		// Verify that the label was added:
		metrics := server.Metrics()
		Expect(metrics).To(MatchLine(
			expectedMetricRegex,
		))
	},
	Entry(
		"Starting installation phase",
		models.HostStageStartingInstallation,
		`^service_assisted_installer_host_installation_phase_seconds_count\{.*phase="Starting installation".*\} .*$`,
	),
	Entry(
		"Configuring phase",
		models.HostStageConfiguring,
		`^service_assisted_installer_host_installation_phase_seconds_count\{.*phase="Configuring".*\} .*$`,
	),
)
