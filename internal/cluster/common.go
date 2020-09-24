package cluster

import (
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const (
	MinMastersNeededForInstallation = 3
	MinWorkersNeededForInstallation = 1
)

const (
	statusInfoReady                           = "Cluster ready to be installed"
	statusInfoInsufficient                    = "Cluster is not ready for install"
	statusInfoInstalling                      = "Installation in progress"
	statusInfoFinalizing                      = "Finalizing cluster installation"
	statusInfoInstalled                       = "installed"
	statusInfoPreparingForInstallation        = "Preparing cluster for installation"
	statusInfoPreparingForInstallationTimeout = "Preparing cluster for installation timeout"
	statusInfoPendingForInput                 = "User input required"
	statusInfoError                           = "cluster has hosts in error"
)

func updateClusterStatus(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string,
	newStatus string, statusInfo string, extra ...interface{}) (*common.Cluster, error) {
	var cluster *common.Cluster
	var err error

	extra = append(append(make([]interface{}, 0), "status", newStatus, "status_info", statusInfo), extra...)

	if newStatus != srcStatus {
		extra = append(extra, "status_updated_at", strfmt.DateTime(time.Now()))
	}

	if cluster, err = UpdateCluster(log, db, clusterId, srcStatus, extra...); err != nil ||
		swag.StringValue(cluster.Status) != newStatus {
		return nil, errors.Wrapf(err, "failed to update cluster %s state from %s to %s",
			clusterId, srcStatus, newStatus)
	}

	if newStatus != srcStatus {
		log.Infof("cluster %s has been updated with the following updates %+v", clusterId, extra)
	}

	return cluster, nil
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
		return nil, errors.Errorf("failed to update cluster %s. nothing have changed", clusterId)
	}

	var cluster common.Cluster

	if err := db.Preload("Hosts").Take(&cluster, "id = ?", clusterId.String()).Error; err != nil {
		return nil, errors.Wrapf(err, "failed to get cluster %s", clusterId.String())
	}

	return &cluster, nil
}

func getKnownMastersNodesIds(c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {

	var cluster common.Cluster
	var masterNodesIds []*strfmt.UUID
	if err := db.Preload("Hosts").First(&cluster, "id = ?", c.ID).Error; err != nil {
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
