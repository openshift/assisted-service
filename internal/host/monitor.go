package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/requestid"
)

func (m *Manager) HostMonitoring() {
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
		if err := m.RefreshStatus(ctx, host, m.db); err != nil {
			log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
		}
	}
}
