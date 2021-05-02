package host

import (
	"context"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/commonutils"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
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
	defer commonutils.MeasureOperation("HostMonitoring", m.log, m.metricApi)()
	var (
		limit     = m.Config.MonitorBatchSize
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
		monitored int64
	)

	jobs := make(chan *common.Cluster, limit)
	monitorRes := make(chan int64, m.Config.MonitorWorkersNum)

	for i := 0; i < m.Config.MonitorWorkersNum; i++ {
		go m.monitorWorker(ctx, jobs, monitorRes)
	}

	m.monitorWorkManager(log, jobs)

	// closing the channel will make sure that works will stop.
	// it will not break the flow and workers will complete all the tasks until the channel is empty.
	close(jobs)
	// using results channel as a wait group for the workers
	for i := 0; i < m.Config.MonitorWorkersNum; i++ {
		monitored += <-monitorRes
	}
	m.metricApi.MonitoredHostsCount(monitored)
}

// consume clusters from the jobs channel and perform refresh status on each host in a cluster
func (m *Manager) monitorWorker(ctx context.Context, jobs chan *common.Cluster, monitorRes chan int64) {
	var monitored int64
	log := logutil.FromContext(ctx, m.log)
	defer func() { monitorRes <- monitored }()
	// pull jobs until channel is closed
	for c := range jobs {
		// can't use leader election, it will cause a deadlock on the work manager
		// workers need to clean the jobs channel
		for _, host := range c.Hosts {
			if !m.SkipMonitoring(host) {
				monitored += 1
				if err := m.refreshStatusInternal(ctx, host, c, m.db); err != nil {
					log.WithError(err).Errorf("failed to refresh host %s state", *host.ID)
				}
			}
		}
	}
}

// iterate over clusters in batches and push them to a jobs channel for the workers to consume
func (m *Manager) monitorWorkManager(log logrus.FieldLogger, jobs chan *common.Cluster) {
	var offset int
	var limit = m.Config.MonitorBatchSize

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
		if err := m.db.Preload("Hosts", "status in (?)", monitorStates).
			Preload(common.MonitoredOperatorsTable).
			Offset(offset).Limit(limit).Order("id").
			Find(&clusters, "exists (select 1 from hosts where clusters.id = hosts.cluster_id)").Error; err != nil {
			log.WithError(err).Errorf("failed to get clusters for host monitoring")
			return
		}
		if len(clusters) == 0 {
			return
		}
		for i := range clusters {
			jobs <- clusters[i]
		}

		offset += limit
	}
}
