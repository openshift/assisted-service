package host

import (
	"context"
	"sort"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/thoas/go-funk"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

func (m *Manager) initMonitoringQueryGenerator(ctx context.Context) {

	if m.monitorClusterQueryGenerator == nil {
		buildInitialQuery := func(db *gorm.DB) *gorm.DB {
			monitorStates := []string{
				models.HostStatusDiscovering,
				models.HostStatusKnown,
				models.HostStatusDisconnected,
				models.HostStatusInsufficient,
				models.HostStatusPendingForInput,
				models.HostStatusPreparingForInstallation,
				models.HostStatusPreparingFailed,
				models.HostStatusPreparingSuccessful,
				models.HostStatusInstalling,
				models.HostStatusInstallingInProgress,
				models.HostStatusInstallingPendingUserAction,
				models.HostStatusResettingPendingUserAction,
			}

			// monitor following states for limited time, until log collection finished or timed-out
			monitorStatesUntilLogCollection := []string{
				models.HostStatusCancelled,
				models.HostStatusError,
			}
			logCollectionEndStates := []string{
				string(models.LogsStateCompleted),
				string(models.LogsStateTimeout),
				string(models.LogsStateEmpty),
			}

			dbWithCondition := common.LoadClusterTablesFromDB(db)
			dbWithCondition = dbWithCondition.Where(
				`id IN (
                         SELECT clusters.id FROM
                           clusters INNER JOIN hosts ON clusters.id = hosts.cluster_id WHERE
                           clusters.deleted_at IS NULL AND
                           hosts.deleted_at IS NULL AND
                           (
                             (hosts.status in (?) OR (hosts.status in (?) AND hosts.logs_info not in (?))) OR
                             (hosts.status = ? AND clusters.status <> ?)
                           )
                       )`,
				monitorStates, monitorStatesUntilLogCollection, logCollectionEndStates, models.HostStatusInstalled, models.ClusterStatusInstalled)
			return dbWithCondition
		}
		m.monitorClusterQueryGenerator = common.NewMonitorQueryGenerator(m.db, buildInitialQuery, m.Config.MonitorBatchSize)
	}
	if m.monitorInfraEnvQueryGenerator == nil {
		m.monitorInfraEnvQueryGenerator = common.NewInfraEnvMonitorQueryGenerator(m.db, m.Config.MonitorBatchSize)
	}
}

func SortHosts(ctx context.Context, hosts []*models.Host) ([]*models.Host, bool) {
	diskCapacityGiB := func(disks []*models.Disk) int64 {
		return funk.Reduce(disks, func(acc int64, d *models.Disk) int64 {
			if d.InstallationEligibility.Eligible {
				return acc + conversions.BytesToGib(d.SizeBytes)
			} else {
				return acc
			}
		}, int64(0)).(int64)
	}

	cpuCount := func(inventory *models.Inventory) int64 {
		if inventory.CPU != nil {
			return inventory.CPU.Count
		} else {
			return 0
		}
	}

	memInGib := func(inventory *models.Inventory) int64 {
		if inventory.Memory != nil {
			return conversions.BytesToGib(inventory.Memory.UsableBytes)
		} else {
			return 0
		}
	}

	allHostsHasInventory := true
	sort.SliceStable(hosts, func(i, j int) bool {
		inventory_i, _ := common.UnmarshalInventory(ctx, hosts[i].Inventory)
		if inventory_i == nil {
			allHostsHasInventory = false
			return false
		}

		inventory_j, _ := common.UnmarshalInventory(ctx, hosts[j].Inventory)
		if inventory_j == nil {
			allHostsHasInventory = false
			return true
		}

		//(host_cores - 4) + ((host_ram_gb - 16) * 0.1) + ((host_disk_capacity_gb - 100) * 0.004)
		wi := 1.0*(float64(cpuCount(inventory_i))-HostWeightMinimumCpuCores) +
			HostWeightMemWeight*(float64(memInGib(inventory_i))-HostWeightMinimumMemGib) +
			HostWeightDiskWeight*(float64(diskCapacityGiB(inventory_i.Disks))-HostWeightMinimumDiskCapacityGib)

		wj := 1.0*(float64(cpuCount(inventory_j))-HostWeightMinimumCpuCores) +
			HostWeightMemWeight*(float64(memInGib(inventory_j))-HostWeightMinimumMemGib) +
			HostWeightDiskWeight*(float64(diskCapacityGiB(inventory_j.Disks))-HostWeightMinimumDiskCapacityGib)

		return wi < wj
	})
	return hosts, allHostsHasInventory
}

func (m *Manager) resetRoleAssignmentIfNotAllRolesAreSet(ctx context.Context) {

	inactiveStatus := []string{models.HostStatusDisconnected, models.HostStatusDisabled}
	if m.leaderElector.IsLeader() {
		clusetersWithMissingRoleAssignmentQuery := m.db.Distinct("cluster_id").
			Where("role = ? and (suggested_role = ? or suggested_role = '' or suggested_role is null)", models.HostRoleAutoAssign, models.HostRoleAutoAssign).
			Where("status NOT IN (?)", inactiveStatus).
			Where("kind != ?", models.HostKindAddToExistingClusterHost).
			Where("deleted_at is null").
			Where("EXISTS (select 1 from clusters where clusters.id = cluster_id and clusters.deleted_at is null)").
			Table("hosts")
		count, reset_err := common.ResetAutoAssignRoles(ctx, m.db, clusetersWithMissingRoleAssignmentQuery)
		if reset_err != nil {
			m.log.WithError(reset_err).Errorf("fail to reset auto-assign role in monitor")
		}
		if count > 0 {
			m.log.Infof("resetting auto-assign roles on %d hosts in monitor", count)
		}
	}
}

func (m *Manager) clusterHostMonitoring(ctx context.Context) int64 {
	var (
		monitored int64
		requestID = requestid.NewID()
		log       = requestid.RequestIDLogger(m.log, requestID)
		clusters  []*common.Cluster
		err       error
	)
	rCtx := requestid.ToContext(ctx, requestID)

	m.resetRoleAssignmentIfNotAllRolesAreSet(rCtx)
	query := m.monitorClusterQueryGenerator.NewClusterQuery(rCtx)
	for {
		if clusters, err = query.Next(rCtx); err != nil {
			m.log.WithError(err).Error("Getting clusters")
			break
		}

		if len(clusters) == 0 {
			break
		}

		log.Debug("Started cluster host monitoring cycle")

		for _, c := range clusters {
			inventoryCache := make(InventoryCache)
			sortedHosts, canRefreshRoles := SortHosts(rCtx, c.Hosts)

			log = log.WithField("cluster", c.ID.String())

			for _, host := range sortedHosts {
				log = log.WithField("host", host.ID.String())
				if !m.leaderElector.IsLeader() {
					log.Debug("Not a leader, exiting cluster HostMonitoring")
					return monitored
				}

				monitored += 1

				log.Debug("Started refreshing host status")
				err = m.refreshStatusInternal(rCtx, host, c, nil, inventoryCache, m.db)
				if err != nil {
					log.WithError(err).Error("failed to refresh host state")
				}
				log.Debug("Finished refreshing host status")

				//the refreshed role will be taken into account in the validations
				//on the next monitor cycle. The roles will not be calculated until
				//all the hosts in the cluster has inventory to avoid race condition
				//with the reset auto-assign mechanism.
				if canRefreshRoles {
					log.Debug()
					err = m.refreshRoleInternal(rCtx, host, m.db, false)
					if err != nil {
						log.WithError(err).Error("failed to refresh host role")
					}
				}
			}
		}

		m.log.Debug("Finished cluster host monitoring cycle")
	}
	return monitored
}

func (m *Manager) infraEnvHostMonitoring(ctx context.Context) int64 {

	var (
		monitored int64
		requestID = requestid.NewID()
		log       = requestid.RequestIDLogger(m.log, requestID)
		infraEnvs []*common.InfraEnv
		err       error
	)
	rCtx := requestid.ToContext(context.Background(), requestID)
	monitorStates := []string{
		models.HostStatusBinding,
		models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscoveringUnbound,
		models.HostStatusInsufficientUnbound,
		models.HostStatusKnownUnbound,
		models.HostStatusReclaiming,
		models.HostStatusReclaimingRebooting,
	}

	query := m.monitorInfraEnvQueryGenerator.NewInfraEnvQuery(rCtx)
	for {
		if infraEnvs, err = query.Next(rCtx); err != nil {
			m.log.WithError(err).Error("Getting infra-envs")
			break
		}

		if len(infraEnvs) == 0 {
			break
		}

		for _, i := range infraEnvs {
			inventoryCache := make(InventoryCache)
			for _, host := range i.Hosts {
				if !m.leaderElector.IsLeader() {
					m.log.Debugf("Not a leader, exiting infra-env HostMonitoring")
					return monitored
				}
				if funk.ContainsString(monitorStates, swag.StringValue(host.Status)) {
					monitored += 1
					err = m.refreshStatusInternal(rCtx, &host.Host, nil, i, inventoryCache, m.db)
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
	ctx := context.Background()
	ctx, span := otel.Tracer("assisted-service").Start(ctx, "Manager.HostMonitoring")
	defer span.End()

	var monitored int64
	if !m.leaderElector.IsLeader() {
		m.log.Debugf("Not a leader, exiting HostMonitoring")
		return
	}
	defer commonutils.MeasureOperation("HostMonitoring", m.log, m.metricApi)()
	m.initMonitoringQueryGenerator(ctx)
	monitored += m.clusterHostMonitoring(ctx)
	monitored += m.infraEnvHostMonitoring(ctx)
	m.metricApi.MonitoredHostsCount(monitored)
}
