package host

import (
	"context"

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

func (m *Manager) initMonitoringQueryGenerator() {
	if m.monitorClusterQueryGenerator == nil {
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
		dbWithCondition := m.db.Preload("Hosts", "status in (?)", monitorStates).Preload(common.MonitoredOperatorsTable).
			Where("exists (select 1 from hosts where clusters.id = hosts.cluster_id)")
		m.monitorClusterQueryGenerator = common.NewMonitorQueryGenerator(m.db, dbWithCondition, m.Config.MonitorBatchSize)
	}
	if m.monitorInfraEnvQueryGenerator == nil {
		m.monitorInfraEnvQueryGenerator = common.NewInfraEnvMonitorQueryGenerator(m.db, m.Config.MonitorBatchSize)
	}
}

func (m *Manager) clusterHostMonitoring() int64 {
	var (
		monitored int64
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
		clusters  []*common.Cluster
		err       error
	)

	query := m.monitorClusterQueryGenerator.NewClusterQuery()
	for {
		if clusters, err = query.Next(); err != nil {
			m.log.WithError(err).Error("Getting clusters")
			break
		}

		if len(clusters) == 0 {
			break
		}

		for _, c := range clusters {
			for _, host := range c.Hosts {
				if !m.leaderElector.IsLeader() {
					m.log.Debugf("Not a leader, exiting cluster HostMonitoring")
					return monitored
				}
				if !m.SkipMonitoring(host) {
					monitored += 1
					err = m.refreshStatusInternal(ctx, host, c, nil, m.db)
					if err != nil {
						log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
					}
				}
			}
		}
	}
	return monitored
}

func (m *Manager) infraEnvHostMonitoring() int64 {
	var (
		monitored int64
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
		infraEnvs []*common.InfraEnv
		err       error
	)
	monitorStates := []string{
		models.HostStatusBinding,
		models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscoveringUnbound,
		models.HostStatusInsufficientUnbound,
		models.HostStatusKnownUnbound,
	}

	query := m.monitorInfraEnvQueryGenerator.NewInfraEnvQuery()
	for {
		if infraEnvs, err = query.Next(); err != nil {
			m.log.WithError(err).Error("Getting infra-envs")
			break
		}

		if len(infraEnvs) == 0 {
			break
		}

		for _, i := range infraEnvs {
			for _, host := range i.Hosts {
				if !m.leaderElector.IsLeader() {
					m.log.Debugf("Not a leader, exiting infra-env HostMonitoring")
					return monitored
				}
				if funk.ContainsString(monitorStates, swag.StringValue(host.Status)) {
					monitored += 1
					err = m.refreshStatusInternal(ctx, &host.Host, nil, i, m.db)
					if err != nil {
						log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
					}
				}
			}
		}
	}
	return monitored
}

func (m *Manager) HostMonitoring() {
	var monitored int64
	if !m.leaderElector.IsLeader() {
		m.log.Debugf("Not a leader, exiting HostMonitoring")
		return
	}
	m.log.Debugf("Running HostMonitoring")
	defer commonutils.MeasureOperation("HostMonitoring", m.log, m.metricApi)()
	m.initMonitoringQueryGenerator()
	monitored += m.clusterHostMonitoring()
	monitored += m.infraEnvHostMonitoring()
	m.metricApi.MonitoredHostsCount(monitored)
}
