package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/requestid"
	"github.com/go-openapi/swag"
)

func (m *Manager) HostMonitoring() {
	var (
		hosts            []*models.Host
		hostAfterRefresh *models.Host
		requestID        = requestid.NewID()
		ctx              = requestid.ToContext(context.Background(), requestID)
		log              = requestid.RequestIDLogger(m.log, requestID)
	)

	monitorStates := []string{HostStatusDiscovering, HostStatusKnown, HostStatusDisconnected, HostStatusInsufficient}
	if err := m.db.Where("status IN (?)", monitorStates).Find(&hosts).Error; err != nil {
		log.WithError(err).Errorf("failed to get hosts")
		return
	}
	for _, host := range hosts {
		state, err := m.getCurrentState(swag.StringValue(host.Status))
		if err != nil {
			log.WithError(err).Errorf("failed to get host %s current state", host.ID)
			continue

		}
		if hostAfterRefresh, err = state.RefreshStatus(ctx, host, m.db); err != nil {
			log.WithError(err).Errorf("failed to refresh host %s state", host.ID)
			continue
		}

		if hostAfterRefresh.Status != host.Status {
			log.Infof("host %s updated status from %s to %s via monitor", host.ID,
				*host.Status, *hostAfterRefresh.Status)
		}
	}
}
