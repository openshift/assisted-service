package metrics

import (
	"fmt"
	"strings"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Collection on scrape", func() {

	const (
		dirname1 = "/data"
		dirname2 = "/data2"
		dirname3 = "/data3"
	)

	var (
		server                  *MetricsServer
		ctrl                    *gomock.Controller
		diskStatsHelper         *MockDiskStatsHelper
		log                     *logrus.Logger
		collector               *directoryUsageCollector
		directory1expectedUsage = uint64(1024)
		directory2expectedUsage = uint64(2048)
		directory3expectedUsage = uint64(4096)
		directory1expectedFree  = uint64(32768)
		directory2expectedFree  = uint64(65536)
		directory3expectedFree  = uint64(131072)
	)

	BeforeEach(func() {
		server = NewMetricsServer()
		ctrl = gomock.NewController(GinkgoT())
		diskStatsHelper = NewMockDiskStatsHelper(ctrl)
		diskStatsHelper.EXPECT().GetDiskUsage(dirname1).Return(directory1expectedUsage, directory1expectedFree, nil).AnyTimes()
		diskStatsHelper.EXPECT().GetDiskUsage(dirname2).Return(directory2expectedUsage, directory2expectedFree, nil).AnyTimes()
		diskStatsHelper.EXPECT().GetDiskUsage(dirname3).Return(directory3expectedUsage, directory3expectedFree, nil).AnyTimes()

		log = logrus.New()
		collector = newDirectoryUsageCollector([]string{dirname1, dirname2, dirname3}, diskStatsHelper, log)
		server.registry.MustRegister(collector)
	})

	AfterEach(func() {
		server.Close()
	})

	var expectMetricValue = func(metric string, value string) {
		metricParts := strings.Split(metric, "}")
		metricValue := strings.Trim(metricParts[1], " ")
		Expect(metricValue).To(Equal(value))
	}

	It("should collect metrics about disk usage on retrieval of metrics", func() {
		retrievedMetrics := server.Metrics()
		count := 0
		for _, metric := range retrievedMetrics {
			if strings.HasPrefix(metric, directoryUsageUsedBytesName) {
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname1)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory1expectedUsage))
				}
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname2)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory2expectedUsage))
				}
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname3)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory3expectedUsage))
				}
				count++
			}
			if strings.HasPrefix(metric, fsUsageFreeBytesName) {
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname1)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory1expectedFree))
				}
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname2)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory2expectedFree))
				}
				if strings.Contains(metric, fmt.Sprintf("directory=\"%s\"", dirname3)) {
					expectMetricValue(metric, fmt.Sprintf("%d", directory3expectedFree))
				}
				count++
			}
		}
		Expect(count).To(BeEquivalentTo(6))
	})
})
