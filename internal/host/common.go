package host

import (
	"time"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	statusInfoDisconnected = "Host keepalive timeout"
	statusInfoDisabled     = "Host is disabled"
	statusInfoDiscovering  = "Waiting for host hardware info"
	statusInfoInstalling   = "Installation in progress"
)

type UpdateReply struct {
	State     string
	IsChanged bool
}

type baseState struct {
	log logrus.FieldLogger
	db  *gorm.DB
}

func updateState(log logrus.FieldLogger, state, stateInfo string, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return updateStateWithParams(log, state, stateInfo, h, db)
}

func updateByKeepAlive(log logrus.FieldLogger, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	if time.Since(time.Time(h.UpdatedAt)) > 3*time.Minute {
		return updateState(log, HostStatusDisconnected, statusInfoDisconnected, h, db)
	}
	return &UpdateReply{
		State:     swag.StringValue(h.Status),
		IsChanged: false,
	}, nil
}

func updateStateWithParams(log logrus.FieldLogger, status, statusInfo string, h *models.Host, db *gorm.DB, extra ...interface{}) (*UpdateReply, error) {
	updates := map[string]interface{}{"status": status, "status_info": statusInfo}
	if len(extra)%2 != 0 {
		return nil, errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}
	dbReply := db.Model(&models.Host{}).Where("id = ? and cluster_id = ? and status = ?",
		h.ID.String(), h.ClusterID.String(), swag.StringValue(h.Status)).
		Updates(updates)
	if dbReply.Error != nil {
		return nil, errors.Wrapf(dbReply.Error, "failed to update host %s from cluster %s state from %s to %s",
			h.ID.String(), h.ClusterID, swag.StringValue(h.Status), status)
	}
	if dbReply.RowsAffected == 0 {
		return nil, errors.Errorf("failed to update host %s from cluster %s state from %s to %s, nothing have changed",
			h.ID.String(), h.ClusterID, swag.StringValue(h.Status), status)
	}
	log.Infof("Updated host <%s> status from <%s> to <%s> with fields: %s",
		h.ID.String(), swag.StringValue(h.Status), status, updates)
	return &UpdateReply{
		State:     status,
		IsChanged: status != swag.StringValue(h.Status),
	}, nil
}

func updateHwInfo(log logrus.FieldLogger, hwValidator hardware.Validator, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	reply, err := hwValidator.IsSufficient(h)
	if err != nil {
		return nil, err
	}
	if !reply.IsSufficient {
		return updateStateWithParams(log, HostStatusInsufficient, reply.Reason, h, db,
			"hardware_info", h.HardwareInfo)
	}
	return updateStateWithParams(log, HostStatusKnown, "", h, db, "hardware_info", h.HardwareInfo)
}
