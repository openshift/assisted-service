package metrics

import (
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

const (
	directoryUsageUsedBytesName        = "assisted_installer_directory_usage_bytes"
	directoryUsageUsedBytesDescription = "The amount of the free space in a directory that has been used in bytes"
	fsUsageFreeBytesName               = "assisted_installer_fs_free_bytes"
	fsUsageFreeBytesDescription        = "The amount of free space that is remaining for the filesystem backing this directory"

	directoryLabelKey = "directory"
)

type directoryUsageCollector struct {
	// The directory for which to report disk usage stats
	directories             []string
	diskStatsHelper         DiskStatsHelper
	directoryUsageBytesDesc *prometheus.Desc
	fsFreeByteDesc          *prometheus.Desc
	log                     *logrus.Logger
}

func newDirectoryUsageCollector(directories []string, diskStatsHelper DiskStatsHelper, log *logrus.Logger) *directoryUsageCollector {

	sort.Strings(directories)
	directoryUsageBytesDesc := prometheus.NewDesc(directoryUsageUsedBytesName, directoryUsageUsedBytesDescription, []string{directoryLabelKey}, nil)
	fsFreeByteDesc := prometheus.NewDesc(fsUsageFreeBytesName, fsUsageFreeBytesDescription, []string{directoryLabelKey}, nil)

	return &directoryUsageCollector{
		directories:             directories,
		diskStatsHelper:         diskStatsHelper,
		directoryUsageBytesDesc: directoryUsageBytesDesc,
		fsFreeByteDesc:          fsFreeByteDesc,
		log:                     log,
	}
}

func (c *directoryUsageCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.directoryUsageBytesDesc
	ch <- c.fsFreeByteDesc
}

func (c *directoryUsageCollector) Collect(ch chan<- prometheus.Metric) {
	for _, directory := range c.directories {
		usedBytes, freeBytes, err := c.diskStatsHelper.GetDiskUsage(directory)
		if err != nil {
			c.log.WithError(err).Errorf("could not get disk usage information for directory %s", directory)
			return
		}
		ch <- prometheus.MustNewConstMetric(c.directoryUsageBytesDesc, prometheus.GaugeValue, float64(usedBytes), directory)
		ch <- prometheus.MustNewConstMetric(c.fsFreeByteDesc, prometheus.GaugeValue, float64(freeBytes), directory)
	}
}
