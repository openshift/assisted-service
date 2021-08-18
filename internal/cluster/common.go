package cluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/identity"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const (
	MinMastersNeededForInstallation = 3
	MinWorkersNeededForInstallation = 2
)

const (
	StatusInfoReady                           = "Cluster ready to be installed"
	StatusInfoInsufficient                    = "Cluster is not ready for install"
	statusInfoInstalling                      = "Installation in progress"
	statusInfoFinalizing                      = "Finalizing cluster installation"
	statusInfoInstalled                       = "Cluster is installed"
	StatusInfoDegraded                        = "Cluster is installed but degraded"
	statusInfoPreparingForInstallation        = "Preparing cluster for installation"
	statusInfoPreparingForInstallationTimeout = "Preparing cluster for installation timeout"
	statusInfoPendingForInput                 = "User input required"
	statusInfoError                           = "cluster has hosts in error"
	statusInfoTimeout                         = "cluster installation timed out while pending user action (a manual booting from installation disk)"
	statusInfoAddingHosts                     = "cluster is adding hosts to existing OCP cluster"
	statusInfoInstallingPendingUserAction     = "Cluster has hosts with wrong boot order"
	statusInfoUnpreparingHostExists           = "At least one host has stopped preparing for installation"
	statusInfoClusterFailedToPrepare          = "Cluster failed to prepare for installation"
)

func updateClusterStatus(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string,
	newStatus string, statusInfo string, events events.Handler, extra ...interface{}) (*common.Cluster, error) {
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
		msg := fmt.Sprintf("Updated status of cluster %s to %s", cluster.Name, *cluster.Status)
		events.AddEvent(ctx, clusterId, nil, models.EventSeverityInfo, msg, time.Now())
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

	err := db.Model(c).Update(updates).Error
	if err != nil {
		log.WithError(err).Errorf("could not update log progress %v on cluster %s", updates, *c.ID)
		return err
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

	if dbReply.Error != nil || (dbReply.RowsAffected == 0 && !clusterExistsInDB(db, clusterId, updates)) {
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
		if host.Role == models.HostRoleMaster && funk.ContainsString(allowedStatuses, swag.StringValue(host.Status)) {
			masterNodesIds = append(masterNodesIds, host.ID)
		}
	}
	return masterNodesIds, nil
}

func NumberOfWorkers(c *common.Cluster) int {
	num := 0
	for _, host := range c.Hosts {
		if host.Role != models.HostRoleWorker || *host.Status == models.HostStatusDisabled {
			continue
		}
		num += 1
	}
	return num
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
		if role != "" && host.Role != role {
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

// GetCluster returns the cluster entity fetched from the DB or an error if failed to
func GetCluster(ctx context.Context, logger logrus.FieldLogger, db *gorm.DB, clusterID string) (*common.Cluster, *common.ApiErrorResponse) {
	log := logutil.FromContext(ctx, logger)
	var cluster common.Cluster
	if err := db.First(&cluster, identity.AddUserFilter(ctx, "id = ?"), clusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", clusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewApiError(http.StatusNotFound, err)
		}

		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	return &cluster, nil
}

func UpdateMachineCidr(db *gorm.DB, cluster *common.Cluster, machineCidr string) error {
	// Delete previous primary machine CIDR
	if network.IsMachineCidrAvailable(cluster) {
		if err := common.DeleteRecordsByClusterID(db, *cluster.ID, []interface{}{&models.MachineNetwork{}}, "cidr = ?", network.GetMachineCidrById(cluster, 0)); err != nil {
			return err
		}
	}

	if machineCidr != "" {
		if err := db.Model(&models.MachineNetwork{}).Save(&models.MachineNetwork{
			ClusterID: *cluster.ID,
			Cidr:      models.Subnet(machineCidr),
		}).Error; err != nil {
			return err
		}
	}

	return db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Update(&common.Cluster{
		MachineNetworkCidrUpdatedAt: time.Now(),
	}).Error
}
