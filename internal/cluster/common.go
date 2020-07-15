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

func updateState(state string, statusInfo string, c *common.Cluster, db *gorm.DB, log logrus.FieldLogger) (*UpdateReply, error) {
	updates := map[string]interface{}{"status": state, "status_info": statusInfo, "status_updated_at": strfmt.DateTime(time.Now())}
	if *c.Status == clusterStatusReady && state == clusterStatusPrepareForInstallation {
		updates["install_started_at"] = strfmt.DateTime(time.Now())
	}
	dbReply := db.Model(&common.Cluster{}).Where("id = ? and status = ?",
		c.ID.String(), swag.StringValue(c.Status)).Updates(updates)
	if dbReply.Error != nil {
		return nil, errors.Wrapf(dbReply.Error, "failed to update cluster %s state from %s to %s",
			c.ID.String(), swag.StringValue(c.Status), state)
	}
	if dbReply.RowsAffected == 0 {
		return nil, errors.Errorf("failed to update cluster %s state from %s to %s, nothing have changed",
			c.ID.String(), swag.StringValue(c.Status), state)
	}
	log.Infof("updated cluster %s from state <%s> to state <%s>", c.ID.String(), swag.StringValue(c.Status), state)
	return &UpdateReply{
		State:     state,
		IsChanged: state != swag.StringValue(c.Status),
	}, nil
}

func updateClusterStateWithParams(log logrus.FieldLogger, srcStatus, statusInfo string, c *common.Cluster, db *gorm.DB,
	extra ...interface{}) error {

	updates := map[string]interface{}{
		"status":            swag.StringValue(c.Status),
		"status_info":       statusInfo,
		"status_updated_at": strfmt.DateTime(time.Now()),
	}
	if len(extra)%2 != 0 {
		return errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}
	dbReply := db.Model(&models.Cluster{}).Where("id = ? and status = ?", c.ID.String(), srcStatus).Updates(updates)
	if dbReply.Error != nil {
		return errors.Wrapf(dbReply.Error, "failed to update cluster %s state from %s to %s",
			c.ID.String(), srcStatus, swag.StringValue(c.Status))
	}
	if dbReply.RowsAffected == 0 && swag.StringValue(c.Status) != srcStatus {
		return errors.Errorf("failed to update cluster %s state from %s to %s, nothing have changed",
			c.ID.String(), srcStatus, swag.StringValue(c.Status))
	}
	c.StatusInfo = &statusInfo
	log.Infof("Updated cluster <%s> status from <%s> to <%s> with fields: %s",
		c.ID.String(), srcStatus, swag.StringValue(c.Status), updates)
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
