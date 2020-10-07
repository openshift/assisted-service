package metrics

import (
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"

	"github.com/alecthomas/units"

	"github.com/openshift/assisted-service/models"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=metricsManager.go -package=metrics -destination=mock_netricsManager_api.go

//////////////////////////////////////////
// counters name and description
/////////////////////////////////////////
const (
	counterClusterCreation              = "assisted_installer_cluster_creations"
	counterClusterInstallationStarted   = "assisted_installer_cluster_installation_started"
	counterClusterInstallationSeconds   = "assisted_installer_cluster_installation_seconds"
	counterOperationDurationMiliSeconds = "assisted_installer_operation_duration_miliseconds"
	counterHostInstallationPhaseSeconds = "assisted_installer_host_installation_phase_seconds"
	counterClusterHosts                 = "assisted_installer_cluster_hosts"
	counterClusterHostCores             = "assisted_installer_cluster_host_cores"
	counterClusterHostRAMGb             = "assisted_installer_cluster_host_ram_gb"
	counterClusterHostDiskGb            = "assisted_installer_cluster_host_disk_gb"
	counterClusterHostNicGb             = "assisted_installer_cluster_host_nic_gb"
)

const (
	counterDescriptionClusterCreation              = "Number of cluster resources created, by version"
	counterDescriptionClusterInstallationStarted   = "Number of clusters that entered installing state, by version"
	counterDescriptionClusterInstallationSeconds   = "Histogram/sum/count of installation time for completed clusters, by result and OCP version"
	counterDescriptionOperationDurationMiliSeconds = "Histogram/sum/count of operation time for specific operation, by name"
	counterDescriptionHostInstallationPhaseSeconds = "Histogram/sum/count of time for each phase, by phase, final install result, and OCP version"
	counterDescriptionClusterHosts                 = "Number of hosts for completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostCores             = "Histogram/sum/count of CPU cores in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostRAMGb             = "Histogram/sum/count of physical RAM in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostDiskGb            = "Histogram/sum/count of installation disk capacity in hosts of completed clusters, by type, raid (level), role, result, and OCP version"
	counterDescriptionClusterHostNicGb             = "Histogram/sum/count of management network NIC speed in hosts of completed clusters, by role, result, and OCP version"
)

const (
	namespace                  = ""
	subsystem                  = "service"
	openshiftVersionLabel      = "openshiftVersion"
	clusterIdLabel             = "clusterId"
	resultLabel                = "result"
	operation                  = "operation"
	phaseLabel                 = "phase"
	roleLabel                  = "role"
	diskTypeLabel              = "diskType"
	discoveryAgentVersionLabel = "discoveryAgentVersion"
	hwVendorLabel              = "vendor"
	hwProductLabel             = "product"
)

type API interface {
	ClusterRegistered(clusterVersion string, clusterID strfmt.UUID)
	InstallationStarted(clusterVersion string, clusterID strfmt.UUID)
	Duration(operation string, duration time.Duration)
	ClusterInstallationFinished(log logrus.FieldLogger, result, clusterVersion string, clusterID strfmt.UUID, installationStratedTime strfmt.DateTime)
	ReportHostInstallationMetrics(log logrus.FieldLogger, clusterVersion string, clusterID strfmt.UUID, boot *models.Disk, h *models.Host, previousProgress *models.HostProgressInfo, currentStage models.HostStage)
}

type MetricsManager struct {
	registry prometheus.Registerer

	serviceLogicClusterCreation              *prometheus.CounterVec
	serviceLogicClusterInstallationStarted   *prometheus.CounterVec
	serviceLogicClusterInstallationSeconds   *prometheus.HistogramVec
	serviceLogicOperationDurationMiliSeconds *prometheus.HistogramVec
	serviceLogicHostInstallationPhaseSeconds *prometheus.HistogramVec
	serviceLogicClusterHosts                 *prometheus.CounterVec
	serviceLogicClusterHostCores             *prometheus.HistogramVec
	serviceLogicClusterHostRAMGb             *prometheus.HistogramVec
	serviceLogicClusterHostDiskGb            *prometheus.HistogramVec
	serviceLogicClusterHostNicGb             *prometheus.HistogramVec
}

func NewMetricsManager(registry prometheus.Registerer) *MetricsManager {

	m := &MetricsManager{
		registry: registry,

		serviceLogicClusterCreation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterCreation,
				Help:      counterDescriptionClusterCreation,
			}, []string{openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterInstallationStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterInstallationStarted,
				Help:      counterDescriptionClusterInstallationStarted,
			}, []string{openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterInstallationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterInstallationSeconds,
			Help:      counterDescriptionClusterInstallationSeconds,
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 900, 1200, 1800},
		}, []string{resultLabel, openshiftVersionLabel, clusterIdLabel}),

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
		}, []string{phaseLabel, resultLabel, openshiftVersionLabel, clusterIdLabel, discoveryAgentVersionLabel, hwVendorLabel, hwProductLabel, diskTypeLabel}),

		serviceLogicClusterHosts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterHosts,
				Help:      counterDescriptionClusterHosts,
			}, []string{roleLabel, resultLabel, openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterHostCores: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostCores,
			Help:      counterDescriptionClusterHostCores,
			Buckets:   []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterHostRAMGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostRAMGb,
			Help:      counterDescriptionClusterHostRAMGb,
			Buckets:   []float64{8, 16, 32, 64, 128, 256, 512, 1024, 2048},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterHostDiskGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostDiskGb,
			Help:      counterDescriptionClusterHostDiskGb,
			Buckets:   []float64{250, 500, 1000, 2000, 4000, 8000, 16000},
		}, []string{diskTypeLabel, roleLabel, resultLabel, openshiftVersionLabel, clusterIdLabel}),

		serviceLogicClusterHostNicGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostNicGb,
			Help:      counterDescriptionClusterHostNicGb,
			Buckets:   []float64{1, 10, 20, 40, 100},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel, clusterIdLabel}),
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
	)
	return m
}

func (m *MetricsManager) ClusterRegistered(clusterVersion string, clusterID strfmt.UUID) {
	m.serviceLogicClusterCreation.WithLabelValues(clusterVersion, clusterID.String()).Inc()
}
func (m *MetricsManager) InstallationStarted(clusterVersion string, clusterID strfmt.UUID) {
	m.serviceLogicClusterInstallationStarted.WithLabelValues(clusterVersion, clusterID.String()).Inc()
}

func (m *MetricsManager) ClusterInstallationFinished(log logrus.FieldLogger, result, clusterVersion string, clusterID strfmt.UUID, installationStratedTime strfmt.DateTime) {
	duration := time.Since(time.Time(installationStratedTime)).Seconds()
	log.Infof("Cluster %s Installation Finished result %s clusterVersion %s duration %f", clusterID.String(), result, clusterVersion, duration)
	m.serviceLogicClusterInstallationSeconds.WithLabelValues(result, clusterVersion, clusterID.String()).Observe(duration)
}

func (m *MetricsManager) Duration(operation string, duration time.Duration) {
	m.serviceLogicOperationDurationMiliSeconds.WithLabelValues(operation).Observe(float64(duration.Milliseconds()))
}

func (m *MetricsManager) ReportHostInstallationMetrics(log logrus.FieldLogger, clusterVersion string, clusterID strfmt.UUID, boot *models.Disk, h *models.Host,
	previousProgress *models.HostProgressInfo, currentStage models.HostStage) {
	if previousProgress != nil && previousProgress.CurrentStage != currentStage {
		roleStr := string(h.Role)
		if h.Bootstrap {
			roleStr = "bootstrap"
		}
		installationStageStr := string(currentStage)
		switch currentStage {
		case models.HostStageDone, models.HostStageFailed:
			m.handleHostInstallationComplete(log, clusterVersion, clusterID, roleStr, installationStageStr, h)
		}
		//report the installation phase duration
		if previousProgress.CurrentStage != "" {
			duration := time.Since(time.Time(previousProgress.StageStartedAt)).Seconds()
			phaseResult := models.HostStageDone
			if currentStage == models.HostStageFailed {
				phaseResult = models.HostStageFailed
			}

			var hwInfo models.Inventory
			var hwVendor, hwProduct string = "Unknown", "Unknown"
			if err := json.Unmarshal([]byte(h.Inventory), &hwInfo); err == nil {
				if hwInfo.SystemVendor != nil {
					hwVendor = hwInfo.SystemVendor.Manufacturer
					hwProduct = hwInfo.SystemVendor.ProductName
				}
			}

			var diskType string = "Unknown"
			if boot != nil {
				diskType = boot.DriveType
			}
			log.Infof("service Logic Host Installation Phase Seconds phase %s, vendor %s product %s disk %s result %s, duration %f",
				string(previousProgress.CurrentStage), hwVendor, hwProduct, diskType, string(phaseResult), duration)
			m.serviceLogicHostInstallationPhaseSeconds.WithLabelValues(string(previousProgress.CurrentStage),
				string(phaseResult), clusterVersion, clusterID.String(), h.DiscoveryAgentVersion, hwVendor, hwProduct, diskType).Observe(duration)
		}
	}
}

func (m *MetricsManager) handleHostInstallationComplete(log logrus.FieldLogger, clusterVersion string, clusterID strfmt.UUID, roleStr string, installationStageStr string, h *models.Host) {
	log.Infof("service Logic Cluster Hosts clusterVersion %s, roleStr %s, result %s",
		clusterVersion, roleStr, installationStageStr)
	m.serviceLogicClusterHosts.WithLabelValues(roleStr, installationStageStr, clusterVersion, clusterID.String()).Inc()
	var hwInfo models.Inventory

	err := json.Unmarshal([]byte(h.Inventory), &hwInfo)
	if err != nil {
		log.Errorf("failed to report host hardware installation metrics for %s", h.ID)
	} else {
		log.Infof("service Logic Cluster Host Cores role %s, result %s cpu %d",
			roleStr, installationStageStr, hwInfo.CPU.Count)
		m.serviceLogicClusterHostCores.WithLabelValues(roleStr, installationStageStr,
			clusterVersion, clusterID.String()).Observe(float64(hwInfo.CPU.Count))
		log.Infof("service Logic Cluster Host RAMGb role %s, result %s ram %d",
			roleStr, installationStageStr, bytesToGib(hwInfo.Memory.PhysicalBytes))
		m.serviceLogicClusterHostRAMGb.WithLabelValues(roleStr, installationStageStr,
			clusterVersion, clusterID.String()).Observe(float64(bytesToGib(hwInfo.Memory.PhysicalBytes)))
		for _, disk := range hwInfo.Disks {
			//TODO change the code after adding storage controller to disk model
			diskTypeStr := disk.DriveType //+ "-" + disk.StorageController
			log.Infof("service Logic Cluster Host DiskGb role %s, result %s diskType %s diskSize %d",
				roleStr, installationStageStr, diskTypeStr, bytesToGib(disk.SizeBytes))
			//TODO missing raid data
			m.serviceLogicClusterHostDiskGb.WithLabelValues(diskTypeStr, roleStr, installationStageStr,
				clusterVersion, clusterID.String()).Observe(float64(bytesToGib(disk.SizeBytes)))
		}
		for _, inter := range hwInfo.Interfaces {
			log.Infof("service Logic Cluster Host NicGb role %s, result %s SpeedMbps %f",
				roleStr, installationStageStr, float64(inter.SpeedMbps))
			m.serviceLogicClusterHostNicGb.WithLabelValues(roleStr, installationStageStr,
				clusterVersion, clusterID.String()).Observe(float64(inter.SpeedMbps))
		}
	}
}

func bytesToGib(bytes int64) int64 {
	return bytes / int64(units.GiB)
}
