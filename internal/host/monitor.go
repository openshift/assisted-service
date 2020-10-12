package host

import (
	"context"

	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/requestid"
)

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
	}
	for {
		//for offset = 0; offset < count; offset += limit {
		hosts := make([]*models.Host, 0, limit)
		if err := m.db.Where("status IN (?)", monitorStates).Offset(offset).Limit(limit).
			Order("cluster_id, id").Find(&hosts).Error; err != nil {
			log.WithError(err).Errorf("failed to get hosts")
			return
		}
		if len(hosts) == 0 {
			break
		}
		for i := range hosts {
			if !m.leaderElector.IsLeader() {
				m.log.Debugf("Not a leader, exiting HostMonitoring")
				return
			}
			if err := m.RefreshStatus(ctx, hosts[i], m.db); err != nil {
				log.WithError(err).Errorf("failed to refresh host %s state", *hosts[i].ID)
			}
		}
		offset += limit
	}
}
