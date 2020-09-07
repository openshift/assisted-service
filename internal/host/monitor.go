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
		hosts     []*models.Host
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

	if err := m.db.Where("status IN (?)", monitorStates).Find(&hosts).Error; err != nil {
		log.WithError(err).Errorf("failed to get hosts")
		return
	}
	for _, host := range hosts {
		if !m.leaderElector.IsLeader() {
			m.log.Debugf("Not a leader, exiting HostMonitoring")
			return
		}
		if err := m.RefreshStatus(ctx, host, m.db); err != nil {
			log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
		}
	}
}
