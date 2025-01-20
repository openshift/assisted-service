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
	"github.com/sirupsen/logrus"
)

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
		metricsManagerConfig := &MetricsManagerConfig{
			DirectoryUsageMonitorConfig: DirectoryUsageMonitorConfig{
				Directories: []string{"/data"}}}
		manager := NewMetricsManager(server.Registry(), handler, NewOSDiskStatsHelper(), metricsManagerConfig, logrus.New())
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
