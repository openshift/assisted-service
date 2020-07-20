package metrics

import (
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"

	"github.com/alecthomas/units"

	"github.com/filanov/bm-inventory/models"
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
	counterDescriptionHostInstallationPhaseSeconds = "Histogram/sum/count of time for each phase, by phase, final install result, and OCP version"
	counterDescriptionClusterHosts                 = "Number of hosts for completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostCores             = "Histogram/sum/count of CPU cores in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostRAMGb             = "Histogram/sum/count of physical RAM in hosts of completed clusters, by role, result, and OCP version"
	counterDescriptionClusterHostDiskGb            = "Histogram/sum/count of installation disk capacity in hosts of completed clusters, by type, raid (level), role, result, and OCP version"
	counterDescriptionClusterHostNicGb             = "Histogram/sum/count of management network NIC speed in hosts of completed clusters, by role, result, and OCP version"
)

const (
	namespace             = ""
	subsystem             = "service"
	openshiftVersionLabel = "openshiftVersion"
	resultLabel           = "result"
	phaseLabel            = "phase"
	roleLabel             = "role"
	diskTypeLabel         = "diskType"
)

type API interface {
	ClusterRegistered(clusterVersion string)
	InstallationStarted(clusterVersion string)
	ClusterInstallationFinished(log logrus.FieldLogger, result, clusterVersion string, installationStratedTime strfmt.DateTime)
	ReportHostInstallationMetrics(log logrus.FieldLogger, clusterVersion string, h *models.Host, previousProgress *models.HostProgressInfo, currentStage models.HostStage)
}

type MetricsManager struct {
	registry prometheus.Registerer

	serviceLogicClusterCreation              *prometheus.CounterVec
	serviceLogicClusterInstallationStarted   *prometheus.CounterVec
	serviceLogicClusterInstallationSeconds   *prometheus.HistogramVec
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
			}, []string{openshiftVersionLabel}),

		serviceLogicClusterInstallationStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterInstallationStarted,
				Help:      counterDescriptionClusterInstallationStarted,
			}, []string{openshiftVersionLabel}),

		serviceLogicClusterInstallationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterInstallationSeconds,
			Help:      counterDescriptionClusterInstallationSeconds,
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 40, 50, 60, 90, 120, 150, 180, 210, 240, 270, 300, 360, 420, 480, 540,
				600, 900, 1200, 1500, 1800, 2100, 2400, 2700, 3000, 3300, 3600},
		}, []string{resultLabel, openshiftVersionLabel}),

		serviceLogicHostInstallationPhaseSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterHostInstallationPhaseSeconds,
			Help:      counterDescriptionHostInstallationPhaseSeconds,
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 40, 50, 60, 90, 120, 150, 180, 210, 240, 270, 300, 360, 420, 480, 540,
				600, 900, 1200, 1500, 1800, 2100, 2400, 2700, 3000, 3300, 3600},
		}, []string{phaseLabel, resultLabel, openshiftVersionLabel}),

		serviceLogicClusterHosts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterHosts,
				Help:      counterDescriptionClusterHosts,
			}, []string{roleLabel, resultLabel, openshiftVersionLabel}),

		serviceLogicClusterHostCores: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostCores,
			Help:      counterDescriptionClusterHostCores,
			Buckets:   []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel}),

		serviceLogicClusterHostRAMGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostRAMGb,
			Help:      counterDescriptionClusterHostRAMGb,
			Buckets:   []float64{8, 16, 32, 64, 128, 256, 512, 1024, 2048},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel}),

		serviceLogicClusterHostDiskGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostDiskGb,
			Help:      counterDescriptionClusterHostDiskGb,
			Buckets:   []float64{250, 500, 1000, 2000, 4000, 8000, 16000},
		}, []string{diskTypeLabel, roleLabel, resultLabel, openshiftVersionLabel}),

		serviceLogicClusterHostNicGb: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      counterClusterHostNicGb,
			Help:      counterDescriptionClusterHostNicGb,
			Buckets:   []float64{1, 10, 20, 40, 100},
		}, []string{roleLabel, resultLabel, openshiftVersionLabel}),
	}

	registry.MustRegister(
		m.serviceLogicClusterCreation,
		m.serviceLogicClusterInstallationStarted,
		m.serviceLogicClusterInstallationSeconds,
		m.serviceLogicHostInstallationPhaseSeconds,
		m.serviceLogicClusterHosts,
		m.serviceLogicClusterHostCores,
		m.serviceLogicClusterHostRAMGb,
		m.serviceLogicClusterHostDiskGb,
		m.serviceLogicClusterHostNicGb,
	)
	return m
}

func (m *MetricsManager) ClusterRegistered(clusterVersion string) {
	m.serviceLogicClusterCreation.WithLabelValues(clusterVersion).Inc()
}
func (m *MetricsManager) InstallationStarted(clusterVersion string) {
	m.serviceLogicClusterInstallationStarted.WithLabelValues(clusterVersion).Inc()
}

func (m *MetricsManager) ClusterInstallationFinished(log logrus.FieldLogger, result, clusterVersion string, installationStratedTime strfmt.DateTime) {
	duration := time.Since(time.Time(installationStratedTime)).Seconds()
	log.Infof("Cluster Installation Finished result %s clusterVersion %s duration %f", result, clusterVersion, duration)
	m.serviceLogicClusterInstallationSeconds.WithLabelValues(result, clusterVersion).Observe(duration)
}

func (m *MetricsManager) ReportHostInstallationMetrics(log logrus.FieldLogger, clusterVersion string, h *models.Host,
	previousProgress *models.HostProgressInfo, currentStage models.HostStage) {

	if previousProgress != nil && previousProgress.CurrentStage != currentStage {

		roleStr := string(h.Role)
		if h.Bootstrap {
			roleStr = "bootstrap"
		}
		installationStageStr := string(currentStage)
		switch currentStage {
		case models.HostStageDone, models.HostStageFailed:
			m.handleHostInstallationComplete(log, clusterVersion, roleStr, installationStageStr, h)
		}
		//report the installation phase duration
		if previousProgress.CurrentStage != "" {
			duration := time.Since(time.Time(previousProgress.StageStartedAt)).Seconds()
			phaseResult := models.HostStageDone
			if currentStage == models.HostStageFailed {
				phaseResult = models.HostStageFailed
			}
			log.Infof("service Logic Host Installation Phase Seconds phase %s, result %s, duration %f",
				string(previousProgress.CurrentStage), string(phaseResult), duration)
			m.serviceLogicHostInstallationPhaseSeconds.WithLabelValues(string(previousProgress.CurrentStage),
				string(phaseResult), clusterVersion).Observe(duration)
		}
	}
}

func (m *MetricsManager) handleHostInstallationComplete(log logrus.FieldLogger, clusterVersion string, roleStr string, installationStageStr string, h *models.Host) {
	log.Infof("service Logic Cluster Hosts clusterVersion %s, roleStr %s, result %s",
		clusterVersion, roleStr, installationStageStr)
	m.serviceLogicClusterHosts.WithLabelValues(roleStr, installationStageStr, clusterVersion).Inc()
	var hwInfo models.Inventory

	err := json.Unmarshal([]byte(h.Inventory), &hwInfo)
	if err != nil {
		log.Errorf("failed to report host hardware installation metrics for %s", h.ID)
	} else {
		log.Infof("service Logic Cluster Host Cores role %s, result %s cpu %d",
			roleStr, installationStageStr, hwInfo.CPU.Count)
		m.serviceLogicClusterHostCores.WithLabelValues(roleStr, installationStageStr,
			clusterVersion).Observe(float64(hwInfo.CPU.Count))
		log.Infof("service Logic Cluster Host RAMGb role %s, result %s ram %d",
			roleStr, installationStageStr, bytesToGib(hwInfo.Memory.PhysicalBytes))
		m.serviceLogicClusterHostRAMGb.WithLabelValues(roleStr, installationStageStr,
			clusterVersion).Observe(float64(bytesToGib(hwInfo.Memory.PhysicalBytes)))
		for _, disk := range hwInfo.Disks {
			//TODO change the code after adding storage controller to disk model
			diskTypeStr := disk.DriveType //+ "-" + disk.StorageController
			log.Infof("service Logic Cluster Host DiskGb role %s, result %s diskType %s diskSize %d",
				roleStr, installationStageStr, diskTypeStr, bytesToGib(disk.SizeBytes))
			//TODO missing raid data
			m.serviceLogicClusterHostDiskGb.WithLabelValues(diskTypeStr, roleStr, installationStageStr,
				clusterVersion).Observe(float64(bytesToGib(disk.SizeBytes)))
		}
		for _, inter := range hwInfo.Interfaces {
			log.Infof("service Logic Cluster Host NicGb role %s, result %s SpeedMbps %f",
				roleStr, installationStageStr, float64(inter.SpeedMbps))
			m.serviceLogicClusterHostNicGb.WithLabelValues(roleStr, installationStageStr,
				clusterVersion).Observe(float64(inter.SpeedMbps))
		}
	}
}

func bytesToGib(bytes int64) int64 {
	return bytes / int64(units.GiB)
}
