package cluster

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MinMastersNeededForInstallation = 3
)

const (
	StatusInfoReady                           = "Cluster ready to be installed"
	StatusInfoInsufficient                    = "Cluster is not ready for install"
	statusInfoInstalling                      = "Installation in progress"
	statusInfoFinalizing                      = "Finalizing cluster installation"
	statusInfoInstalled                       = "Cluster is installed"
	StatusInfoDegraded                        = "Cluster is installed but degraded"
	StatusInfoNotAllWorkersInstalled          = "Cluster is installed but some workers did not join"
	statusInfoPreparingForInstallation        = "Preparing cluster for installation"
	statusInfoPreparingForInstallationTimeout = "Preparing cluster for installation timeout"
	statusInfoFinalizingTimeout               = "Cluster installation timeout while finalizing"
	statusInfoPendingForInput                 = "User input required"
	statusInfoError                           = "cluster has hosts in error"
	statusInfoTimeout                         = "cluster installation timed out while pending user action (a manual booting from installation disk)"
	statusInfoAddingHosts                     = "cluster is adding hosts to existing OCP cluster"
	statusInfoInstallingPendingUserAction     = "Cluster has hosts pending user action"
	statusInfoUnpreparingHostExists           = "At least one host has stopped preparing for installation"
	statusInfoClusterFailedToPrepare          = "Cluster failed to prepare for installation"
)

func updateClusterStatus(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string,
	newStatus string, statusInfo string, events eventsapi.Handler, extra ...interface{}) (*common.Cluster, error) {
	var cluster *common.Cluster
	var err error
	extra = append(append(make([]interface{}, 0), "status", newStatus, "status_info", statusInfo), extra...)

	if newStatus != srcStatus {
		now := strfmt.DateTime(time.Now())
		extra = append(extra, "status_updated_at", now)

		installationCompletedStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError, models.ClusterStatusCancelled}
		if funk.ContainsString(installationCompletedStatuses, swag.StringValue(&newStatus)) {
			extra = append(extra, "install_completed_at", now)
		}
	}

	if newStatus != srcStatus {
		extra = append(extra, "trigger_monitor_timestamp", time.Now())
	}

	if cluster, err = UpdateCluster(log, db, clusterId, srcStatus, extra...); err != nil ||
		swag.StringValue(cluster.Status) != newStatus {
		return nil, errors.Wrapf(err, "failed to update cluster %s state from %s to %s",
			clusterId, srcStatus, newStatus)
	}

	if newStatus != srcStatus {
		eventgen.SendClusterStatusUpdatedEvent(ctx, events, clusterId, *cluster.Status, statusInfo)
		log.Infof("cluster %s has been updated with the following updates %+v", clusterId, extra)
	}

	return cluster, nil
}

func updateLogsProgress(log logrus.FieldLogger, db *gorm.DB, c *common.Cluster, progress string) error {
	var updates map[string]interface{}

	switch progress {
	case string(models.LogsStateRequested):
		updates = map[string]interface{}{
			"logs_info":                  progress,
			"controller_logs_started_at": strfmt.DateTime(time.Now()),
		}
	default:
		updates = map[string]interface{}{
			"logs_info": progress,
		}
	}

	result := db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).Updates(updates)

	if err := result.Error; err != nil {
		log.WithError(err).Errorf("could not update log progress %v on cluster %s", updates, *c.ID)
		return err
	}
	if result.RowsAffected == 1 {
		updatedCluster, err := common.GetClusterFromDB(db, *c.ID, common.UseEagerLoading)
		if err != nil {
			log.WithError(err).Errorf("could not update log progress %v on cluster %s", updates, *c.ID)
			return err
		}
		*c = *updatedCluster
	}

	log.Infof("cluster %s has been updated with the following log progress %+v", *c.ID, updates)
	return nil
}

func ClusterExists(db *gorm.DB, clusterId strfmt.UUID) bool {
	where := make(map[string]interface{})
	return clusterExistsInDB(db, clusterId, where)
}

func clusterExistsInDB(db *gorm.DB, clusterId strfmt.UUID, where map[string]interface{}) bool {
	where["id"] = clusterId.String()
	var cluster common.Cluster
	return db.Select("id").Take(&cluster, where).Error == nil
}

func UpdateCluster(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string, extra ...interface{}) (*common.Cluster, error) {
	updates := make(map[string]interface{})

	if len(extra)%2 != 0 {
		return nil, errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}

	// Query by <cluster-id, status>
	// Status is required as well to avoid races between different components.
	dbReply := db.Model(&common.Cluster{}).Where("id = ? and status = ?", clusterId, srcStatus).Updates(updates)

	if dbReply.Error != nil {
		return nil, errors.Wrapf(dbReply.Error, "failed to update cluster %s", clusterId)
	}

	if dbReply.RowsAffected == 0 && !clusterExistsInDB(db, clusterId, updates) {
		return nil, errors.Errorf("failed to update cluster %s. nothing has changed", clusterId)
	}

	return common.GetClusterFromDB(db, clusterId, common.UseEagerLoading)
}

func getKnownMastersNodesIds(c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	var cluster *common.Cluster
	var err error
	var masterNodesIds []*strfmt.UUID
	if cluster, err = common.GetClusterFromDB(db, *c.ID, common.UseEagerLoading); err != nil {
		return nil, errors.Errorf("cluster %s not found", c.ID)
	}

	allowedStatuses := []string{models.HostStatusKnown, models.HostStatusPreparingForInstallation}
	for _, host := range cluster.Hosts {
		if common.GetEffectiveRole(host) == models.HostRoleMaster && funk.ContainsString(allowedStatuses, swag.StringValue(host.Status)) {
			masterNodesIds = append(masterNodesIds, host.ID)
		}
	}
	return masterNodesIds, nil
}

func NumberOfWorkers(c *common.Cluster) int {
	num := 0
	for _, host := range c.Hosts {
		if common.GetEffectiveRole(host) != models.HostRoleWorker {
			continue
		}
		num += 1
	}
	return num
}

func HostsInStatus(c *common.Cluster, statuses []string) (int, int) {
	mappedMastersByRole := MapMasterHostsByStatus(c)
	mappedWorkersByRole := MapWorkersHostsByStatus(c)
	mastersInSomeInstallingStatus := 0
	workersInSomeInstallingStatus := 0

	for _, status := range statuses {
		mastersInSomeInstallingStatus += len(mappedMastersByRole[status])
		workersInSomeInstallingStatus += len(mappedWorkersByRole[status])
	}
	return mastersInSomeInstallingStatus, workersInSomeInstallingStatus
}

func MapMasterHostsByStatus(c *common.Cluster) map[string][]*models.Host {
	return mapHostsByStatus(c, models.HostRoleMaster)
}

func MapWorkersHostsByStatus(c *common.Cluster) map[string][]*models.Host {
	return mapHostsByStatus(c, models.HostRoleWorker)
}

func mapHostsByStatus(c *common.Cluster, role models.HostRole) map[string][]*models.Host {
	hostMap := make(map[string][]*models.Host)
	for _, host := range c.Hosts {
		if role != "" && common.GetEffectiveRole(host) != role {
			continue
		}
		if _, ok := hostMap[swag.StringValue(host.Status)]; ok {
			hostMap[swag.StringValue(host.Status)] = append(hostMap[swag.StringValue(host.Status)], host)
		} else {
			hostMap[swag.StringValue(host.Status)] = []*models.Host{host}
		}
	}
	return hostMap
}

func MapHostsByStatus(c *common.Cluster) map[string][]*models.Host {
	return mapHostsByStatus(c, "")
}

func UpdateMachineCidr(db *gorm.DB, cluster *common.Cluster, machineCidr string) error {
	// In case of dual-stack clusters the autocalculation feature is not supported. That means
	// as soon as we detect that current Machine Network configuration indicates we have such
	// a cluster, the function stops its execution.
	reqDualStack := network.CheckIfClusterIsDualStack(cluster)
	if reqDualStack {
		return nil
	}

	previousPrimaryMachineCidr := ""
	if network.IsMachineCidrAvailable(cluster) {
		previousPrimaryMachineCidr = network.GetMachineCidrById(cluster, 0)
	}

	if machineCidr != previousPrimaryMachineCidr {
		if machineCidr != "" {
			// MGMT-8853: Nothing is done when there's a conflict since there's no change to what's being inserted/updated.
			if err := db.Clauses(clause.OnConflict{
				DoNothing: true,
			}).Create(&models.MachineNetwork{
				ClusterID: *cluster.ID,
				Cidr:      models.Subnet(machineCidr),
			}).Error; err != nil {
				return err
			}
		}

		// Delete previous primary machine CIDR
		if network.IsMachineCidrAvailable(cluster) {
			if err := common.DeleteRecordsByClusterID(db, *cluster.ID, []interface{}{&models.MachineNetwork{}}, "cidr = ?", network.GetMachineCidrById(cluster, 0)); err != nil {
				return err
			}
		}

		return db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(map[string]interface{}{
			"machine_network_cidr_updated_at": time.Now(),
		}).Error
	}
	return nil
}

func machineNetworksAlreadyExist(cluster *common.Cluster, machineNetworks []string) bool {
	if len(cluster.MachineNetworks) != len(machineNetworks) {
		return false
	}
	for _, m := range cluster.MachineNetworks {
		if !funk.ContainsString(machineNetworks, string(m.Cidr)) {
			return false
		}
	}
	return true
}

func updateMachineNetworks(db *gorm.DB, cluster *common.Cluster, machineNetworks []string) error {
	if len(machineNetworks) == 0 || machineNetworksAlreadyExist(cluster, machineNetworks) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.MachineNetwork{}, "cluster_id = ?", cluster.ID.String()).Error; err != nil {
			return err
		}
		if err := tx.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Update("machine_network_cidr_updated_at", time.Now()).Error; err != nil {
			return err
		}
		for _, m := range machineNetworks {
			if err := tx.Create(&models.MachineNetwork{ClusterID: *cluster.ID, Cidr: models.Subnet(m)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
