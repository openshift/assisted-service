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

func updateClusterStatus(status string, statusInfo string, c *common.Cluster, db *gorm.DB, log logrus.FieldLogger) (*UpdateReply, error) {
	wouldChange := status != swag.StringValue(c.Status)

	if err := updateClusterStateWithParams(log, status, statusInfo, c, db); err != nil {
		return nil, err
	}

	return &UpdateReply{
		State:     status,
		IsChanged: wouldChange,
	}, nil
}

func updateClusterStateWithParams(log logrus.FieldLogger, status string, statusInfo string, c *common.Cluster, db *gorm.DB,
	extra ...interface{}) error {

	updates := map[string]interface{}{"status": status, "status_info": statusInfo}

	if status != swag.StringValue(c.Status) {
		updates["status_updated_at"] = strfmt.DateTime(time.Now())
	}
	if *c.Status == clusterStatusReady && status == clusterStatusPrepareForInstallation {
		updates["install_started_at"] = strfmt.DateTime(time.Now())
	} else if *c.Status == clusterStatusInstalling && status == clusterStatusInstalled {
		updates["install_completed_at"] = strfmt.DateTime(time.Now())
	}

	if len(extra)%2 != 0 {
		return errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}

	// Query by <cluster-id, status>
	// Status is queried as well to avoid races between different components.
	dbReply := db.Model(&common.Cluster{}).Where("id = ? and status = ?",
		c.ID.String(), swag.StringValue(c.Status)).Updates(updates)
	if dbReply.Error != nil {
		return errors.Wrapf(dbReply.Error, "failed to update cluster %s state from %s to %s",
			c.ID.String(), swag.StringValue(c.Status), status)
	}
	if dbReply.RowsAffected == 0 {
		return errors.Errorf("failed to update cluster %s state from %s to %s, nothing have changed",
			c.ID.String(), swag.StringValue(c.Status), status)
	}
	log.Infof("updated cluster %s from state <%s> to state <%s>", c.ID.String(), swag.StringValue(c.Status), status)

	if err := db.First(c, "id = ?", c.ID.String()).Error; err != nil {
		return errors.Wrapf(dbReply.Error, "failed to read from cluster %s from the database after the update",
			c.ID.String())
	}

	return nil
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
