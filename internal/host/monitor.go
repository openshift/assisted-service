package host

import (
	"context"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/thoas/go-funk"
)

func (m *Manager) SkipMonitoring(h *models.Host) bool {
	skipMonitoringStates := []string{string(models.LogsStateCompleted), string(models.LogsStateTimeout), ""}
	result := ((swag.StringValue(h.Status) == models.HostStatusError || swag.StringValue(h.Status) == models.HostStatusCancelled) &&
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
		skipped   = 0
		limit     = m.Config.MonitorBatchSize
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
		err       error
	)

	defer commonutils.MeasureOperation(fmt.Sprintf("host HostMonitoring num of hosts %d, skipped %d", offset, skipped), m.log, m.metricApi)()

	monitorStates := []string{
		models.HostStatusDiscovering,
		models.HostStatusKnown,
		models.HostStatusDisconnected,
		models.HostStatusInsufficient,
		models.HostStatusPendingForInput,
		models.HostStatusPreparingForInstallation,
		models.HostStatusPreparingSuccessful,
		models.HostStatusInstalling,
		models.HostStatusInstallingInProgress,
		models.HostStatusInstalled,
		models.HostStatusInstallingPendingUserAction,
		models.HostStatusResettingPendingUserAction,
		models.HostStatusCancelled, // for limited time, until log collection finished or timed-out
		models.HostStatusError,     // for limited time, until log collection finished or timed-out
	}
	for {
		var clusters []*common.Cluster
		if err = m.db.Preload("Hosts", "status in (?)", monitorStates).Preload(common.MonitoredOperatorsTable).
			Offset(offset).Limit(limit).Order("id").Find(&clusters, "exists (select 1 from hosts where clusters.id = hosts.cluster_id)").Error; err != nil {
			log.WithError(err).Errorf("failed to get clusters for host monitoring")
			return
		}
		if len(clusters) == 0 {
			break
		}
		for _, c := range clusters {
			for _, host := range c.Hosts {
				if !m.leaderElector.IsLeader() {
					m.log.Debugf("Not a leader, exiting HostMonitoring")
					return
				}
				if !m.SkipMonitoring(host) {
					err = m.refreshStatusInternal(ctx, host, c, m.db)
					if err != nil {
						log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
					}
				} else {
					skipped++
				}
			}
		}
		offset += limit
	}
}
