package cluster

import (
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const (
	clusterStatusInsufficient           = "insufficient"
	clusterStatusReady                  = "ready"
	clusterStatusPrepareForInstallation = "preparing-for-installation"
	clusterStatusInstalling             = "installing"
	clusterStatusInstalled              = "installed"
	clusterStatusError                  = "error"
)

const (
	statusInfoReady                           = "Cluster ready to be installed"
	statusInfoInsufficient                    = "cluster is insufficient, exactly 3 known master hosts are needed for installation"
	statusInfoInstalling                      = "Installation in progress"
	statusInfoFinalizing                      = "Finalizing cluster installation"
	statusInfoInstalled                       = "installed"
	statusInfoPreparingForInstallation        = "Preparing cluster for installation"
	statusInfoPreparingForInstallationTimeout = "Preparing cluster for installation timeout"
)

type UpdateReply struct {
	State     string
	IsChanged bool
}

type baseState struct {
	//TODO remove when res: https://github.com/golangci/golangci-lint/issues/537
	log logrus.FieldLogger //nolint:structcheck
	db  *gorm.DB           //nolint:structcheck
}

func updateClusterStatusUpdateReplay(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string,
	newStatus string, statusInfo string) (*UpdateReply, error) {
	wouldChange := srcStatus != newStatus

	if err := updateClusterStatus(log, db, clusterId, srcStatus, newStatus, statusInfo); err != nil {
		return nil, err
	}

	return &UpdateReply{
		State:     newStatus,
		IsChanged: wouldChange,
	}, nil
}

func updateClusterStatus(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, srcStatus string,
	newStatus string, statusInfo string, extra ...interface{}) error {
	extra = append(extra, "status", newStatus, "status_info", statusInfo)

	if newStatus != srcStatus {
		extra = append(extra, "status_updated_at", strfmt.DateTime(time.Now()))
	}

	if _, err := UpdateCluster(log, db, clusterId, srcStatus, extra...); err != nil {
		return errors.Wrapf(err, "failed to update cluster %s state from %s to %s",
			clusterId, srcStatus, newStatus)
	}

	return nil
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

	if dbReply.Error != nil || dbReply.RowsAffected == 0 {
		return nil, errors.Errorf("failed to update cluster %s. nothing have changed", clusterId)
	}
	log.Infof("cluster %s has been updated with the following updateds %+v", clusterId, extra)

	var cluster common.Cluster

	if err := db.First(&cluster, "id = ?", clusterId).Error; err != nil {
		return nil, errors.Wrapf(dbReply.Error, "failed to read from cluster %s from the database after the update",
			clusterId)
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

func mapMasterHostsByStatus(c *common.Cluster) map[string][]*models.Host {
	hostMap := make(map[string][]*models.Host)

	for _, host := range c.Hosts {
		if host.Role != models.HostRoleMaster {
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
