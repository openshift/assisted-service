package host

import (
	"context"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/thoas/go-funk"
)

func (m *Manager) SkipMonitoring(h *models.Host) bool {
	skipMonitoringStates := []string{string(models.LogsStateCompleted), string(models.LogsStateTimeout), ""}
	result := ((swag.StringValue(h.Status) == models.HostStatusError || swag.StringValue(h.Status) == models.HostStatusCanceled) &&
		funk.Contains(skipMonitoringStates, h.LogsInfo))
	return result
}

func (m *Manager) HostMonitoring() {
	if !m.leaderElector.IsLeader() {
		m.log.Debugf("Not a leader, exiting HostMonitoring")
		return
	}
	m.log.Debugf("Running HostMonitoring")
	var (
		offset    int
		limit     = m.Config.MonitorBatchSize
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
	)

	monitorStates := []string{
		models.HostStatusDiscovering,
		models.HostStatusKnown,
		models.HostStatusDisconnected,
		models.HostStatusInsufficient,
		models.HostStatusPendingForInput,
		models.HostStatusPreparingForInstallation,
		models.HostStatusInstalling,
		models.HostStatusInstallingInProgress,
		models.HostStatusInstalled,
		models.HostStatusInstallingPendingUserAction,
		models.HostStatusResettingPendingUserAction,
		models.HostStatusCanceled, // for limited time, until log collection finished or timed-out
		models.HostStatusError,    // for limited time, until log collection finished or timed-out
	}
	for {
		hosts := make([]*models.Host, 0, limit)
		if err := m.db.Where("status IN (?)", monitorStates).Offset(offset).Limit(limit).
			Order("cluster_id, id").Find(&hosts).Error; err != nil {
			log.WithError(err).Errorf("failed to get hosts")
			return
		}
		if len(hosts) == 0 {
			break
		}
		for _, host := range hosts {
			if !m.leaderElector.IsLeader() {
				m.log.Debugf("Not a leader, exiting HostMonitoring")
				return
			}
			if !m.SkipMonitoring(host) {
				if err := m.RefreshStatus(ctx, host, m.db); err != nil {
					log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
				}
			}
		}
		offset += limit
	}
}
