package host

import (
	"context"
	"encoding/json"

	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

type HostMetricsReporterAPI interface {
	ReportValidationFailedMetrics(ctx context.Context, h *models.Host) error
	ReportHostInstallationMetrics(ctx context.Context, clusterID strfmt.UUID, boot *models.Disk,
		h *models.Host, previousProgress *models.HostProgressInfo, currentStage models.HostStage)
}

type HostMetricsReporter struct {
	log           logrus.FieldLogger
	metricsApi    metrics.API
	eventsHandler eventsapi.Handler
}

func NewHostMetricsReporter(log logrus.FieldLogger, metricsApi metrics.API, eventsHandler eventsapi.API) *HostMetricsReporter {
	return &HostMetricsReporter{
		log:           log,
		metricsApi:    metricsApi,
		eventsHandler: eventsHandler,
	}
}

func (m *HostMetricsReporter) ReportValidationFailedMetrics(ctx context.Context, h *models.Host) error {
	log := logutil.FromContext(ctx, m.log)
	if h.ValidationsInfo == "" {
		log.Warnf("Host %s in cluster %s doesn't contain any validations info, cannot report metrics for that host", h.ID, h.ClusterID)
		return nil
	}
	var validationRes ValidationsStatus
	if err := json.Unmarshal([]byte(h.ValidationsInfo), &validationRes); err != nil {
		log.WithError(err).Errorf("Failed to unmarshal validations info from host %s in cluster %s", h.ID, h.ClusterID)
		return err
	}
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.Status == ValidationFailure {
				m.metricsApi.HostValidationFailed(models.HostValidationID(v.ID))
			}
		}
	}
	return nil
}

func (m *HostMetricsReporter) ReportHostInstallationMetrics(ctx context.Context, clusterID strfmt.UUID, boot *models.Disk,
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

		diskType := models.DriveTypeUnknown
		if boot != nil {
			diskType = boot.DriveType
		}
		switch currentStage {
		case models.HostStageDone, models.HostStageFailed:
			//TODO: handle cancel as well
			m.reportHostMetricsOnInstallationComplete(ctx, clusterID, roleStr, hwVendor, hwProduct, string(diskType), installationStageStr, h)
		}
		//report the installation phase duration
		if previousProgress.CurrentStage != "" {
			duration := time.Since(time.Time(previousProgress.StageStartedAt))
			phaseResult := models.HostStageDone
			if currentStage == models.HostStageFailed {
				phaseResult = models.HostStageFailed
			}
			log.Infof("service Logic Host Installation Phase Seconds phase %s, vendor %s product %s disk %s result %s, duration %f",
				string(previousProgress.CurrentStage), hwVendor, hwProduct, diskType, string(phaseResult), duration)
			m.eventsHandler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "host.stage.duration", time.Now(),
				"result", string(phaseResult), "duration", duration, "host_stage", string(previousProgress.CurrentStage), "vendor", hwVendor, "product", hwProduct, "disk_type", diskType, "host_role", roleStr)

			m.metricsApi.InstallationPhaseSeconds(string(previousProgress.CurrentStage), string(phaseResult), duration)
		}
	}
}

func (m *HostMetricsReporter) reportHostMetricsOnInstallationComplete(ctx context.Context, clusterID strfmt.UUID,
	roleStr string, hwVendor string, hwProduct string, diskType string, installationStageStr string, h *models.Host) {
	log := logutil.FromContext(ctx, logrus.New())

	//increment the count of successful installed hosts
	log.Infof("service Logic Cluster Hosts, roleStr %s, vendor %s, product %s, disk %s, result %s",
		roleStr, hwVendor, hwProduct, diskType, installationStageStr)
	m.serviceLogicClusterHosts.WithLabelValues(roleStr, installationStageStr).Inc()

	var hwInfo models.Inventory
	err := json.Unmarshal([]byte(h.Inventory), &hwInfo)
	if err != nil {
		log.Errorf("failed to report host hardware installation metrics for %s", h.ID)
		return
	}
	//collect the number of host's cores
	log.Infof("service Logic Cluster Host Cores role %s, result %s cpu %d",
		roleStr, installationStageStr, hwInfo.CPU.Count)

	m.serviceLogicClusterHostCores.WithLabelValues(roleStr, installationStageStr).
		Observe(float64(hwInfo.CPU.Count))

	//collect the host's RAM data
	log.Infof("service Logic Cluster Host RAMGb role %s, result %s ram %d",
		roleStr, installationStageStr, bytesToGib(hwInfo.Memory.PhysicalBytes))

	m.serviceLogicClusterHostRAMGb.WithLabelValues(roleStr, installationStageStr).
		Observe(float64(bytesToGib(hwInfo.Memory.PhysicalBytes)))

	m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "host.mem.cpu", time.Now(),
		"host_result", installationStageStr, "host_role", roleStr, "mem_bytes", bytesToGib(hwInfo.Memory.PhysicalBytes),
		"core_count", hwInfo.CPU.Count)

	//report disk's type, size and role for each disk
	for _, disk := range hwInfo.Disks {
		//TODO change the code after adding storage controller to disk model
		//TODO missing raid data
		diskTypeStr := string(disk.DriveType) //+ "-" + disk.StorageController
		log.Infof("service Logic Cluster Host DiskGb role %s, result %s diskType %s diskSize %d",
			roleStr, installationStageStr, diskTypeStr, bytesToGib(disk.SizeBytes))
		m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "disk.size.type", time.Now(),
			"host_result", installationStageStr, "host_role", roleStr, "disk_type", diskTypeStr, "disk_size", bytesToGib(disk.SizeBytes))

		m.serviceLogicClusterHostDiskGb.WithLabelValues(diskTypeStr, roleStr, installationStageStr).
			Observe(float64(bytesToGib(disk.SizeBytes)))
	}
	//report NIC's speed. role for each NIC
	for _, inter := range hwInfo.Interfaces {
		log.Infof("service Logic Cluster Host NicGb role %s, result %s SpeedMbps %f",
			roleStr, installationStageStr, float64(inter.SpeedMbps))
		m.handler.AddMetricsEvent(ctx, clusterID, h.ID, models.EventSeverityInfo, "nic.speed", time.Now(),
			"host_result", installationStageStr, "host_role", roleStr, "nic_speed", inter.SpeedMbps)

		m.serviceLogicClusterHostNicGb.WithLabelValues(roleStr, installationStageStr).
			Observe(float64(inter.SpeedMbps))
	}
}
