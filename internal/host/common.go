package host

import (
	"encoding/json"
	"time"

	"github.com/filanov/bm-inventory/internal/validators"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/go-openapi/strfmt"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	statusInfoDisconnected               = "Host keepalive timeout"
	statusInfoDisabled                   = "Host is disabled"
	statusInfoDiscovering                = "Waiting for host hardware info"
	statusInfoInstalling                 = "Installation in progress"
	statusInfoResettingPendingUserAction = "Reboot the host into the installation image to complete resetting the installation"
	statusInfoPreparingForInstallation   = "Preparing host for installation"
)

type UpdateReply struct {
	State     string
	IsChanged bool
}

type baseState struct {
	log logrus.FieldLogger
	db  *gorm.DB
}

func updateByKeepAlive(log logrus.FieldLogger, h *models.Host, db *gorm.DB) (*models.Host, error) {
	if h.CheckedInAt.String() != "" && time.Since(time.Time(h.CheckedInAt)) > 3*time.Minute {
		return updateHostStatus(log, db, h.ClusterID, *h.ID, *h.Status, HostStatusDisconnected, statusInfoDisconnected)
	}
	return h, nil
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

	if host, err = UpdateHost(log, db, clusterId, hostId, srcStatus, extra...); err != nil || *host.Status != newStatus {
		return nil, errors.Wrapf(err, "failed to update host %s from cluster %s state from %s to %s",
			hostId, clusterId, srcStatus, newStatus)
	}

	return host, nil
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

	if dbReply.Error != nil || dbReply.RowsAffected == 0 {
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

func getCluster(clusterID strfmt.UUID, db *gorm.DB) (*common.Cluster, error) {
	var cluster common.Cluster
	if err := db.Preload("Hosts", "status <> ?", HostStatusDisabled).First(&cluster, "id = ?", clusterID).Error; err != nil {
		return nil, err
	}
	return &cluster, nil
}

func isSufficientRole(h *models.Host) *validators.IsSufficientReply {
	var reason string
	isSufficient := true

	if h.Role == "" {
		isSufficient = false
		reason = "No role selected"
	}

	return &validators.IsSufficientReply{
		Type:         "role",
		IsSufficient: isSufficient,
		Reason:       reason,
	}
}

func checkAndUpdateSufficientHost(log logrus.FieldLogger, h *models.Host, db *gorm.DB, hwValidator hardware.Validator,
	connectivityValidator connectivity.Validator) (*models.Host, error) {
	//checking if need to change state to disconnect
	if hostAfterKeepAlive, err := updateByKeepAlive(log, h, db); err != nil || hostAfterKeepAlive.Status != h.Status {
		return hostAfterKeepAlive, err
	}
	var statusInfoDetails = make(map[string]string)
	//checking inventory isInsufficient
	cluster, err := getCluster(h.ClusterID, db)
	if err != nil {
		return nil, err
	}
	inventoryReply, err := hwValidator.IsSufficient(h, cluster)
	if err != nil {
		statusInfoDetails["hardware"] = "parsing error"
	} else {
		statusInfoDetails[inventoryReply.Type] = inventoryReply.Reason
	}

	//checking connectivity isSufficient
	connectivityReply, err := connectivityValidator.IsSufficient(h, cluster)
	if err != nil {
		statusInfoDetails["connectivity"] = "unknown error"
	} else {
		statusInfoDetails[connectivityReply.Type] = connectivityReply.Reason
	}
	//checking role isSufficient
	roleReply := isSufficientRole(h)
	statusInfoDetails[roleReply.Type] = roleReply.Reason

	var newStatus, newStatusInfo string
	if inventoryReply != nil && inventoryReply.IsSufficient && roleReply.IsSufficient && connectivityReply.IsSufficient {
		newStatus = HostStatusKnown
		newStatusInfo = ""
	} else {
		statusInfo, err := json.Marshal(statusInfoDetails)
		if err != nil {
			return nil, err
		}
		newStatus = HostStatusInsufficient
		newStatusInfo = string(statusInfo)
	}

	//update status & status info in DB only if there is a change
	if swag.StringValue(h.Status) != newStatus || swag.StringValue(h.StatusInfo) != newStatusInfo {
		log.Infof("is sufficient host: %s role reply %+v inventory reply %+v connectivity reply %+v", h.ID, roleReply, inventoryReply, connectivityReply)
		return updateHostStatus(log, db, h.ClusterID, *h.ID, *h.Status, newStatus, newStatusInfo)
	}
	return h, nil
}
