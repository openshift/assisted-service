package host

import (
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	statusInfoDisconnected               = "Host keepalive timeout"
	statusInfoDisabled                   = "Host is disabled"
	statusInfoDiscovering                = "Waiting for host hardware info"
	statusInfoInsufficientHardware       = "Host does not pass minimum hardware requirements"
	statusInfoPendingForInput            = "User input required"
	statusInfoNotReadyForInstall         = "Host not ready for install"
	statusInfoInstalling                 = "Installation in progress"
	statusInfoResettingPendingUserAction = "Reboot the host into the installation image to complete resetting the installation"
	statusInfoPreparingForInstallation   = "Preparing host for installation"
	statusInfoPreparingTimedOut          = "Cluster is no longer preparing for installation"
)

type UpdateReply struct {
	State     string
	IsChanged bool
}

func updateHostProgress(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, newStatus string, statusInfo string,
	srcStage models.HostStage, newStage models.HostStage, progressInfo string, extra ...interface{}) (*models.Host, error) {

	extra = append(append(make([]interface{}, 0), "progress_current_stage", newStage, "progress_progress_info", progressInfo,
		"stage_updated_at", strfmt.DateTime(time.Now())), extra...)

	if newStage != srcStage {
		extra = append(extra, "stage_started_at", strfmt.DateTime(time.Now()))
	}

	return updateHostStatus(log, db, clusterId, hostId, srcStatus, newStatus, statusInfo, extra...)
}

func updateHostStatus(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, newStatus string, statusInfo string, extra ...interface{}) (*models.Host, error) {
	var host *models.Host
	var err error

	extra = append(append(make([]interface{}, 0), "status", newStatus, "status_info", statusInfo), extra...)

	if newStatus != srcStatus {
		extra = append(extra, "status_updated_at", strfmt.DateTime(time.Now()))
	}

	if host, err = UpdateHost(log, db, clusterId, hostId, srcStatus, extra...); err != nil ||
		swag.StringValue(host.Status) != newStatus {
		return nil, errors.Wrapf(err, "failed to update host %s from cluster %s state from %s to %s",
			hostId, clusterId, srcStatus, newStatus)
	}

	return host, nil
}

func hostExistsInDB(db *gorm.DB, hostId, clusterId strfmt.UUID, where map[string]interface{}) bool {
	where["id"] = hostId.String()
	where["cluster_id"] = clusterId.String()
	var host models.Host
	return db.Select("id").Take(&host, where).Error == nil
}

func UpdateHost(log logrus.FieldLogger, db *gorm.DB, clusterId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, extra ...interface{}) (*models.Host, error) {
	updates := make(map[string]interface{})

	if len(extra)%2 != 0 {
		return nil, errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}

	// Query by <cluster-id, host-id, status>
	// Status is required as well to avoid races between different components.
	dbReply := db.Model(&models.Host{}).Where("id = ? and cluster_id = ? and status = ?",
		hostId, clusterId, srcStatus).
		Updates(updates)

	if dbReply.Error != nil || (dbReply.RowsAffected == 0 && !hostExistsInDB(db, hostId, clusterId, updates)) {
		return nil, errors.Errorf("failed to update host %s from cluster %s. nothing have changed", hostId, clusterId)
	}
	log.Infof("host %s from cluster %s has been updated with the following updateds %+v", hostId, clusterId, extra)

	var host models.Host

	if err := db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error; err != nil {
		return nil, errors.Wrapf(dbReply.Error, "failed to read from host %s from cluster %s from the database after the update",
			hostId, clusterId)
	}

	return &host, nil
}
