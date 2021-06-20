package metrics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=metricsManager.go -package=metrics -destination=mock_metrics_manager_api.go

//////////////////////////////////////////
// counters name and description
/////////////////////////////////////////
const (
	counterClusterCreation                        = "assisted_installer_cluster_creations"
	counterClusterInstallationStarted             = "assisted_installer_cluster_installation_started"
	counterClusterInstallationSeconds             = "assisted_installer_cluster_installation_seconds"
	counterOperationDurationMiliSeconds           = "assisted_installer_operation_duration_miliseconds"
	counterHostInstallationPhaseSeconds           = "assisted_installer_host_installation_phase_seconds"
	counterClusterHosts                           = "assisted_installer_cluster_hosts"
	counterClusterHostCores                       = "assisted_installer_cluster_host_cores"
	counterClusterHostRAMGb                       = "assisted_installer_cluster_host_ram_gb"
	counterClusterHostDiskGb                      = "assisted_installer_cluster_host_disk_gb"
	counterClusterHostNicGb                       = "assisted_installer_cluster_host_nic_gb"
	counterClusterHostInstallationCount           = "assisted_installer_cluster_host_installation_count"
	counterClusterHostNTPFailuresCount            = "assisted_installer_cluster_host_ntp_failures"
	counterClusterHostDiskSyncDurationMiliSeconds = "assisted_installer_cluster_host_disk_sync_duration_ms"
	counterClusterHostImagePullStatus             = "assisted_installer_cluster_host_image_pull_status"
	counterHostValidationFailed                   = "assisted_installer_host_validation_is_in_failed_status_on_cluster_deletion"
	counterHostValidationChanged                  = "assisted_installer_host_validation_failed_after_success_before_installation"
	counterClusterValidationFailed                = "assisted_installer_cluster_validation_is_in_failed_status_on_cluster_deletion"
	counterClusterValidationChanged               = "assisted_installer_cluster_validation_failed_after_success_before_installation"
	counterFilesystemUsagePercentage              = "assisted_installer_filesystem_usage_percentage"
	counterMonitoredHosts                         = "assisted_installer_monitored_hosts"
	counterMonitoredClusters                      = "assisted_installer_monitored_clusters"
)

const (
	counterDescriptionClusterCreation                        = "Number of cluster resources created, by version"
	counterDescriptionClusterInstallationStarted             = "Number of clusters that entered installing state, by version"
	counterDescriptionClusterHostInstallationCount           = "Number of hosts per cluster"
	counterDescriptionClusterHostNTPFailuresCount            = "Number of NTP failures per cluster"
	counterDescriptionClusterInstallationSeconds             = "Histogram/sum/count of installation time for completed clusters, by result and OCP version"
	counterDescriptionOperationDurationMiliSeconds           = "Histogram/sum/count of operation time for specific operation, by name"
	counterDescriptionHostInstallationPhaseSeconds           = "Histogram/sum/count of time for each phase, by phase, final install result, and OCP version"
	counterDescriptionClusterHosts                           = "Number of hosts for completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostCores                       = "Histogram/sum/count of CPU cores in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostRAMGb                       = "Histogram/sum/count of physical RAM in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostDiskGb                      = "Histogram/sum/count of installation disk capacity in hosts of completed clusters, by type, raid (level), role, result, and OCP version"
	counterDescriptionClusterHostNicGb                       = "Histogram/sum/count of management network NIC speed in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostDiskSyncDurationMiliSeconds = "Histogram/sum/count of the disk's fdatasync duration (fetched from fio)"
	counterDescriptionClusterHostImagePullStatus             = "Histogram/sum/count of the images' pull statuses"
	counterDescriptionHostValidationFailed                   = "Number of host validation errors"
	counterDescriptionHostValidationChanged                  = "Number of host validations that already succeed but start to fail again"
	counterDescriptionClusterValidationFailed                = "Number of cluster validation errors"
	counterDescriptionClusterValidationChanged               = "Number of cluster validations that already succeed but start to fail again"
	counterDescriptionFilesystemUsagePercentage              = "The percentage of the filesystem usage by the service"
	counterDescriptionMonitoredHosts                         = "Number of hosts monitored by host monitor"
	counterDescriptionMonitoredClusters                      = "Number of clusters monitored by cluster monitor"
)

const (
	namespace                  = ""
	subsystem                  = "service"
	UnknownHWValue             = "Unknown"
	openshiftVersionLabel      = "openshiftVersion"
	clusterIdLabel             = "clusterId"
	hostIdLabel                = "hostId"
	emailDomainLabel           = "emailDomain"
	resultLabel                = "result"
	operation                  = "operation"
	phaseLabel                 = "phase"
	roleLabel                  = "role"
	diskTypeLabel              = "diskType"
	diskPathLabel              = "diskPathLabel"
	discoveryAgentVersionLabel = "discoveryAgentVersion"
	hwVendorLabel              = "vendor"
	hwProductLabel             = "product"
	userManagedNetworkingLabel = "userManagedNetworking"
	hostValidationTypeLabel    = "hostValidationType"
	clusterValidationTypeLabel = "clusterValidationType"
	imageLabel                 = "imageName"
	hosts                      = "hosts"
	clusters                   = "clusters"
)

type API interface {
	ClusterRegistered(clusterVersion string, clusterID strfmt.UUID, emailDomain string)
	HostValidationFailed(clusterVersion string, emailDomain string, hostValidationType models.HostValidationID)
	HostValidationChanged(clusterVersion string, emailDomain string, hostValidationType models.HostValidationID)
	ClusterValidationFailed(clusterVersion string, emailDomain string, clusterValidationType models.ClusterValidationID)
	ClusterValidationChanged(clusterVersion string, emailDomain string, clusterValidationType models.ClusterValidationID)
	InstallationStarted(clusterVersion string, clusterID strfmt.UUID, emailDomain string, userManagedNetworking string)
	ClusterHostInstallationCount(emailDomain string, hostCount int, clusterVersion string)
	Duration(operation string, duration time.Duration)
	ClusterInstallationFinished(ctx context.Context, result, clusterVersion string, clusterID strfmt.UUID, emailDomain string, installationStartedTime strfmt.DateTime)
	ReportHostInstallationMetrics(ctx context.Context, clusterVersion string, clusterID strfmt.UUID, emailDomain string, boot *models.Disk, h *models.Host, previousProgress *models.HostProgressInfo, currentStage models.HostStage)
	DiskSyncDuration(hostID strfmt.UUID, diskPath string, syncDuration int64)
	ImagePullStatus(hostID strfmt.UUID, imageName, resultStatus string, downloadRate float64)
	FileSystemUsage(usageInPercentage float64)
	MonitoredHostsCount(monitoredHosts int64)
	MonitoredClusterCount(monitoredClusters int64)
}

type MetricsManager struct {
	registry prometheus.Registerer
	handler  events.Handler

	serviceLogicClusterCreation                        *prometheus.CounterVec
	serviceLogicClusterInstallationStarted             *prometheus.CounterVec
	serviceLogicClusterHostInstallationCount           *prometheus.HistogramVec
	serviceLogicClusterHostNTPFailuresCount            *prometheus.HistogramVec
	serviceLogicClusterInstallationSeconds             *prometheus.HistogramVec
	serviceLogicOperationDurationMiliSeconds           *prometheus.HistogramVec
	serviceLogicHostInstallationPhaseSeconds           *prometheus.HistogramVec
	serviceLogicClusterHosts                           *prometheus.CounterVec
	serviceLogicClusterHostCores                       *prometheus.HistogramVec
	serviceLogicClusterHostRAMGb                       *prometheus.HistogramVec
	serviceLogicClusterHostDiskGb                      *prometheus.HistogramVec
	serviceLogicClusterHostNicGb                       *prometheus.HistogramVec
	serviceLogicClusterHostDiskSyncDurationMiliSeconds *prometheus.HistogramVec
	serviceLogicClusterHostImagePullStatus             *prometheus.HistogramVec
	serviceLogicHostValidationFailed                   *prometheus.CounterVec
	serviceLogicHostValidationChanged                  *prometheus.CounterVec
	serviceLogicClusterValidationFailed                *prometheus.CounterVec
	serviceLogicClusterValidationChanged               *prometheus.CounterVec
	serviceLogicFilesystemUsagePercentage              *prometheus.GaugeVec
	serviceLogicMonitoredHosts                         *prometheus.GaugeVec
	serviceLogicMonitoredClusters                      *prometheus.GaugeVec
}

var _ API = &MetricsManager{}

func NewMetricsManager(registry prometheus.Registerer, eventsHandler events.Handler) *MetricsManager {

	m := &MetricsManager{
		registry: registry,
		handler:  eventsHandler,

		serviceLogicClusterCreation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterCreation,
				Help:      counterDescriptionClusterCreation,
			}, []string{openshiftVersionLabel, clusterIdLabel, emailDomainLabel}),

		serviceLogicClusterInstallationStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterInstallationStarted,
				Help:      counterDescriptionClusterInstallationStarted,
			}, []string{openshiftVersionLabel, clusterIdLabel, emailDomainLabel, userManagedNetworkingLabel}),

		serviceLogicClusterHostInstallationCount: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostInstallationCount,
			Help:      counterDescriptionClusterHostInstallationCount,
		}, []string{openshiftVersionLabel, emailDomainLabel}),

		serviceLogicClusterHostNTPFailuresCount: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostNTPFailuresCount,
			Help:      counterDescriptionClusterHostNTPFailuresCount,
		}, []string{emailDomainLabel}),

		serviceLogicClusterInstallationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterInstallationSeconds,
			Help:      counterDescriptionClusterInstallationSeconds,
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 900, 1200, 1800},
		}, []string{resultLabel, openshiftVersionLabel, clusterIdLabel, emailDomainLabel}),

		serviceLogicOperationDurationMiliSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterOperationDurationMiliSeconds,
			Help:      counterDescriptionOperationDurationMiliSeconds,
			Buckets: []float64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1200,
				1400, 1600, 1800, 2000, 2400, 2800, 3200, 3600, 4000, 5000},
		}, []string{operation}),

		serviceLogicHostInstallationPhaseSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterHostInstallationPhaseSeconds,
			Help:      counterDescriptionHostInstallationPhaseSeconds,
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 900, 1200, 1800},
		}, []string{phaseLabel, resultLabel, openshiftVersionLabel, emailDomainLabel, discoveryAgentVersionLabel, hwVendorLabel, hwProductLabel, diskTypeLabel}),

		serviceLogicClusterHosts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterHosts,
				Help:      counterDescriptionClusterHosts,
			}, []string{roleLabel, resultLabel, openshiftVersionLabel, emailDomainLabel, hwVendorLabel, hwProductLabel, diskTypeLabel}),

		serviceLogicClusterHostCores: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostCores,
			Help:      counterDescriptionClusterHostCores,
			Buckets:   []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, emailDomainLabel}),

		serviceLogicClusterHostRAMGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostRAMGb,
			Help:      counterDescriptionClusterHostRAMGb,
			Buckets:   []float64{8, 16, 32, 64, 128, 256, 512, 1024, 2048},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, emailDomainLabel}),

		serviceLogicClusterHostDiskGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostDiskGb,
			Help:      counterDescriptionClusterHostDiskGb,
			Buckets:   []float64{250, 500, 1000, 2000, 4000, 8000, 16000},
		}, []string{diskTypeLabel, roleLabel, resultLabel, openshiftVersionLabel, emailDomainLabel}),

		serviceLogicClusterHostNicGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostNicGb,
			Help:      counterDescriptionClusterHostNicGb,
			Buckets:   []float64{1, 10, 20, 40, 100},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, emailDomainLabel}),

		serviceLogicClusterHostDiskSyncDurationMiliSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostDiskSyncDurationMiliSeconds,
			Help:      counterDescriptionClusterHostDiskSyncDurationMiliSeconds,
			Buckets:   []float64{1, 5, 10, 15, 20},
		}, []string{diskPathLabel, hostIdLabel}),

		serviceLogicHostValidationFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterHostValidationFailed,
				Help:      counterDescriptionHostValidationFailed,
			}, []string{openshiftVersionLabel, emailDomainLabel, hostValidationTypeLabel}),

		serviceLogicHostValidationChanged: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterHostValidationChanged,
				Help:      counterDescriptionHostValidationChanged,
			}, []string{openshiftVersionLabel, emailDomainLabel, hostValidationTypeLabel}),

		serviceLogicClusterValidationFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterValidationFailed,
				Help:      counterDescriptionClusterValidationFailed,
			}, []string{openshiftVersionLabel, emailDomainLabel, clusterValidationTypeLabel}),

		serviceLogicClusterValidationChanged: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterValidationChanged,
				Help:      counterDescriptionClusterValidationChanged,
			}, []string{openshiftVersionLabel, emailDomainLabel, clusterValidationTypeLabel}),

		serviceLogicClusterHostImagePullStatus: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostImagePullStatus,
			Help:      counterDescriptionClusterHostImagePullStatus,
			Buckets:   []float64{0.1, 0.5, 1, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		}, []string{resultLabel, imageLabel, hostIdLabel}),

		serviceLogicFilesystemUsagePercentage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterFilesystemUsagePercentage,
				Help:      counterDescriptionFilesystemUsagePercentage,
			}, []string{},
		),

		serviceLogicMonitoredHosts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterMonitoredHosts,
			Help:      counterDescriptionMonitoredHosts,
		}, []string{hosts}),

		serviceLogicMonitoredClusters: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterMonitoredClusters,
			Help:      counterDescriptionMonitoredClusters,
		}, []string{hosts}),
	}

	registry.MustRegister(
		m.serviceLogicClusterCreation,
		m.serviceLogicClusterInstallationStarted,
		m.serviceLogicClusterInstallationSeconds,
		m.serviceLogicOperationDurationMiliSeconds,
		m.serviceLogicHostInstallationPhaseSeconds,
		m.serviceLogicClusterHosts,
		m.serviceLogicClusterHostCores,
		m.serviceLogicClusterHostRAMGb,
		m.serviceLogicClusterHostDiskGb,
		m.serviceLogicClusterHostNicGb,
		m.serviceLogicClusterHostDiskSyncDurationMiliSeconds,
		m.serviceLogicHostValidationFailed,
		m.serviceLogicHostValidationChanged,
		m.serviceLogicClusterValidationFailed,
		m.serviceLogicClusterValidationChanged,
		m.serviceLogicClusterHostImagePullStatus,
		m.serviceLogicFilesystemUsagePercentage,
		m.serviceLogicMonitoredHosts,
		m.serviceLogicMonitoredClusters,
	)
	return m
}

func (m *MetricsManager) ClusterRegistered(clusterVersion string, clusterID strfmt.UUID, emailDomain string) {
	m.serviceLogicClusterCreation.WithLabelValues(clusterVersion, clusterID.String(), emailDomain).Inc()
}

func (m *MetricsManager) HostValidationFailed(clusterVersion string, emailDomain string, hostValidationType models.HostValidationID) {
	m.serviceLogicHostValidationFailed.WithLabelValues(clusterVersion, emailDomain, string(hostValidationType)).Inc()
}

func (m *MetricsManager) HostValidationChanged(clusterVersion string, emailDomain string, hostValidationType models.HostValidationID) {
	m.serviceLogicHostValidationChanged.WithLabelValues(clusterVersion, emailDomain, string(hostValidationType)).Inc()
}

func (m *MetricsManager) ClusterValidationFailed(clusterVersion string, emailDomain string, clusterValidationType models.ClusterValidationID) {
	m.serviceLogicClusterValidationFailed.WithLabelValues(clusterVersion, emailDomain, string(clusterValidationType)).Inc()
}

func (m *MetricsManager) ClusterValidationChanged(clusterVersion string, emailDomain string, clusterValidationType models.ClusterValidationID) {
	m.serviceLogicClusterValidationChanged.WithLabelValues(clusterVersion, emailDomain, string(clusterValidationType)).Inc()
}

func (m *MetricsManager) InstallationStarted(clusterVersion string, clusterID strfmt.UUID, emailDomain string, userManagedNetworking string) {
	m.serviceLogicClusterInstallationStarted.WithLabelValues(clusterVersion, clusterID.String(), emailDomain, userManagedNetworking).Inc()
}

func (m *MetricsManager) ClusterHostInstallationCount(emailDomain string, hostCount int, clusterVersion string) {
	m.serviceLogicClusterHostInstallationCount.WithLabelValues(clusterVersion, emailDomain).Observe(float64(hostCount))
}

func (m *MetricsManager) ClusterInstallationFinished(ctx context.Context, result, clusterVersion string, clusterID strfmt.UUID, emailDomain string, installationStartedTime strfmt.DateTime) {
	duration := time.Since(time.Time(installationStartedTime)).Seconds()
	m.handler.AddMetricsEvent(ctx, clusterID, nil, models.EventSeverityInfo, "cluster.installation.results", time.Now(),
		"duration", duration, "result", result)

	log := logutil.FromContext(ctx, logrus.New())
	log.Infof("Cluster %s Installation Finished result %s clusterVersion %s duration %f", clusterID.String(), result, clusterVersion, duration)
	m.serviceLogicClusterInstallationSeconds.WithLabelValues(result, clusterVersion, clusterID.String(), emailDomain).Observe(duration)
}

func (m *MetricsManager) Duration(operation string, duration time.Duration) {
	//MGMT-4526 TODO: Do not move this to ELK with events. It is called repeatedly over short periods of time
	m.serviceLogicOperationDurationMiliSeconds.WithLabelValues(operation).Observe(float64(duration.Milliseconds()))
}

func (m *MetricsManager) DiskSyncDuration(hostID strfmt.UUID, diskPath string, syncDuration int64) {
	m.serviceLogicClusterHostDiskSyncDurationMiliSeconds.WithLabelValues(diskPath, hostID.String()).Observe(float64(syncDuration))
}

func (m *MetricsManager) ImagePullStatus(hostID strfmt.UUID, imageName, resultStatus string, downloadRate float64) {
	m.serviceLogicClusterHostImagePullStatus.WithLabelValues(imageName, resultStatus, hostID.String()).Observe(downloadRate)
}

func (m *MetricsManager) ReportHostInstallationMetrics(ctx context.Context, clusterVersion string, clusterID strfmt.UUID, emailDomain string, boot *models.Disk,
	h *models.Host, previousProgress *models.HostProgressInfo, currentStage models.HostStage) {
	log := logutil.FromContext(ctx, logrus.New())
	if previousProgress != nil && previousProgress.CurrentStage != currentStage {
		roleStr := string(h.Role)
		if h.Bootstrap {
			roleStr = "bootstrap"
		}
		installationStageStr := string(currentStage)

		var hwInfo models.Inventory
		hwVendor, hwProduct := UnknownHWValue, UnknownHWValue
		if err := json.Unmarshal([]byte(h.Inventory), &hwInfo); err == nil {
			if hwInfo.SystemVendor != nil {
				hwVendor = hwInfo.SystemVendor.Manufacturer
				hwProduct = hwInfo.SystemVendor.ProductName
			}
		}

		diskType := UnknownHWValue
		if boot != nil {
			diskType = boot.DriveType
		}
		switch currentStage {
		case models.HostStageDone, models.HostStageFailed:
			//TODO: handle cancel as well
			m.reportHostMetricsOnInstallationComplete(ctx, clusterVersion, clusterID, emailDomain, roleStr, hwVendor, hwProduct, diskType, installationStageStr, h)
		}
		//report the installation phase duration
		if previousProgress.CurrentStage != "" {
			duration := time.Since(time.Time(previousProgress.StageStartedAt)).Seconds()
			phaseResult := models.HostStageDone
			if currentStage == models.HostStageFailed {
				phaseResult = models.HostStageFailed
			}
			log.Infof("service Logic Host Installation Phase Seconds phase %s, vendor %s product %s disk %s result %s, duration %f",
				string(previousProgress.CurrentStage), hwVendor, hwProduct, diskType, string(phaseResult), duration)
			m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "host.stage.duration", time.Now(),
				"result", string(phaseResult), "duration", duration, "host_stage", string(previousProgress.CurrentStage), "vendor", hwVendor, "product", hwProduct, "disk_type", diskType, "host_role", roleStr)

			//MGMT-4526 TODO: remove this scrap after ELK dashboards are verified
			m.serviceLogicHostInstallationPhaseSeconds.WithLabelValues(string(previousProgress.CurrentStage),
				string(phaseResult), clusterVersion, clusterID.String(), emailDomain, h.DiscoveryAgentVersion, hwVendor, hwProduct, diskType).Observe(duration)
		}
	}
}

func (m *MetricsManager) reportHostMetricsOnInstallationComplete(ctx context.Context, clusterVersion string, clusterID strfmt.UUID, emailDomain string,
	roleStr string, hwVendor string, hwProduct string, diskType string, installationStageStr string, h *models.Host) {
	log := logutil.FromContext(ctx, logrus.New())

	//increment the count of successful installed hosts
	log.Infof("service Logic Cluster Hosts clusterVersion %s, roleStr %s, vendor %s, product %s, disk %s, result %s",
		clusterVersion, roleStr, hwVendor, hwProduct, diskType, installationStageStr)
	m.serviceLogicClusterHosts.WithLabelValues(roleStr, installationStageStr, clusterVersion, emailDomain, hwVendor, hwProduct, diskType).Inc()

	var hwInfo models.Inventory
	err := json.Unmarshal([]byte(h.Inventory), &hwInfo)
	if err != nil {
		log.Errorf("failed to report host hardware installation metrics for %s", h.ID)
		return
	}
	//collect the number of host's cores
	log.Infof("service Logic Cluster Host Cores role %s, result %s cpu %d",
		roleStr, installationStageStr, hwInfo.CPU.Count)

	m.serviceLogicClusterHostCores.WithLabelValues(roleStr, installationStageStr,
		clusterVersion, emailDomain).Observe(float64(hwInfo.CPU.Count))

	//collect the host's RAM data
	log.Infof("service Logic Cluster Host RAMGb role %s, result %s ram %d",
		roleStr, installationStageStr, bytesToGib(hwInfo.Memory.PhysicalBytes))

	m.serviceLogicClusterHostRAMGb.WithLabelValues(roleStr, installationStageStr,
		clusterVersion, emailDomain).Observe(float64(bytesToGib(hwInfo.Memory.PhysicalBytes)))

	m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "host.mem.cpu", time.Now(),
		"host_result", installationStageStr, "host_role", roleStr, "mem_bytes", bytesToGib(hwInfo.Memory.PhysicalBytes),
		"core_count", hwInfo.CPU.Count)

	//report disk's type, size and role for each disk
	for _, disk := range hwInfo.Disks {
		//TODO change the code after adding storage controller to disk model
		//TODO missing raid data
		diskTypeStr := disk.DriveType //+ "-" + disk.StorageController
		log.Infof("service Logic Cluster Host DiskGb role %s, result %s diskType %s diskSize %d",
			roleStr, installationStageStr, diskTypeStr, bytesToGib(disk.SizeBytes))
		m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "disk.size.type", time.Now(),
			"host_result", installationStageStr, "host_role", roleStr, "disk_type", diskTypeStr, "disk_size", bytesToGib(disk.SizeBytes))

		m.serviceLogicClusterHostDiskGb.WithLabelValues(diskTypeStr, roleStr, installationStageStr,
			clusterVersion, emailDomain).Observe(float64(bytesToGib(disk.SizeBytes)))
	}
	//report NIC's speed. role for each NIC
	for _, inter := range hwInfo.Interfaces {
		log.Infof("service Logic Cluster Host NicGb role %s, result %s SpeedMbps %f",
			roleStr, installationStageStr, float64(inter.SpeedMbps))
		m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "nic.speed", time.Now(),
			"host_result", installationStageStr, "host_role", roleStr, "nic_speed", inter.SpeedMbps)

		m.serviceLogicClusterHostNicGb.WithLabelValues(roleStr, installationStageStr,
			clusterVersion, emailDomain).Observe(float64(inter.SpeedMbps))
	}
}

func (m *MetricsManager) FileSystemUsage(usageInPercentage float64) {
	m.serviceLogicFilesystemUsagePercentage.WithLabelValues().Set(usageInPercentage)
}

func (m *MetricsManager) MonitoredHostsCount(monitoredHosts int64) {
	m.serviceLogicMonitoredHosts.WithLabelValues(hosts).Set(float64(monitoredHosts))
}

func (m *MetricsManager) MonitoredClusterCount(monitoredClusters int64) {
	m.serviceLogicMonitoredClusters.WithLabelValues(clusters).Set(float64(monitoredClusters))
}

func bytesToGib(bytes int64) int64 {
	return bytes / int64(units.GiB)
}
