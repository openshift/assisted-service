package hostutil

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var ResetLogsField = []interface{}{"logs_info", "", "logs_started_at", strfmt.DateTime(time.Time{}), "logs_collected_at", strfmt.DateTime(time.Time{})}

func UpdateHostProgress(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, clusterId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, newStatus string, statusInfo string,
	srcStage models.HostStage, newStage models.HostStage, progressInfo string, extra ...interface{}) (*common.Host, error) {

	extra = append(append(make([]interface{}, 0), "progress_current_stage", newStage, "progress_progress_info", progressInfo,
		"progress_stage_updated_at", strfmt.DateTime(time.Now())), extra...)

	if newStage != srcStage {
		extra = append(extra, "progress_stage_started_at", strfmt.DateTime(time.Now()))
	}

	return UpdateHostStatus(ctx, log, db, eventsHandler, clusterId, hostId, srcStatus, newStatus, statusInfo, extra...)
}

func UpdateLogsProgress(_ context.Context, log logrus.FieldLogger, db *gorm.DB, _ events.Handler, clusterId strfmt.UUID, hostId strfmt.UUID, srcStatus string, progress string, extra ...interface{}) (*common.Host, error) {
	var host *common.Host
	var err error

	switch progress {
	case string(models.LogsStateRequested):
		extra = append(append(append(make([]interface{}, 0), "logs_info", progress),
			"logs_started_at", strfmt.DateTime(time.Now())), extra...)
	default:
		extra = append(append(make([]interface{}, 0), "logs_info", progress), extra...)
	}

	if host, err = UpdateHost(log, db, clusterId, hostId, srcStatus, extra...); err != nil {
		log.WithError(err).Errorf("failed to update log progress %+v on host %s", extra, hostId)
		return nil, err
	}
	log.Infof("host %s has been updated with the following log progress %+v", hostId, extra)
	return host, nil
}

func UpdateHostStatus(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, infraEnvId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, newStatus string, statusInfo string, extra ...interface{}) (*common.Host, error) {
	var host *common.Host
	var err error

	extra = append(append(make([]interface{}, 0), "status", newStatus, "status_info", statusInfo), extra...)

	if newStatus != srcStatus {
		extra = append(extra, "status_updated_at", strfmt.DateTime(time.Now()))
		extra = append(extra, "trigger_monitor_timestamp", time.Now())
	}

	if host, err = UpdateHost(log, db, infraEnvId, hostId, srcStatus, extra...); err != nil ||
		swag.StringValue(host.Status) != newStatus {
		return nil, errors.Wrapf(err, "failed to update host %s from cluster %s state from %s to %s",
			hostId, infraEnvId, srcStatus, newStatus)
	}

	if newStatus != srcStatus {
		msg := fmt.Sprintf("Host %s: updated status from \"%s\" to \"%s\"", GetHostnameForMsg(&host.Host), srcStatus, newStatus)
		if statusInfo != "" {
			msg += fmt.Sprintf(" (%s)", statusInfo)
		}
		eventsHandler.AddEvent(ctx, infraEnvId, &hostId, GetEventSeverityFromHostStatus(newStatus), msg, time.Now())
		log.Infof("host %s from infra env %s has been updated with the following updates %+v", hostId, infraEnvId, extra)
	}

	return host, nil
}

func UpdateHost(_ logrus.FieldLogger, db *gorm.DB, infraEnvId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, extra ...interface{}) (*common.Host, error) {
	updates := make(map[string]interface{})

	if len(extra)%2 != 0 {
		return nil, errors.Errorf("invalid update extra parameters %+v", extra)
	}
	for i := 0; i < len(extra); i += 2 {
		updates[extra[i].(string)] = extra[i+1]
	}

	// Query by <cluster-id, host-id, status>
	// Status is required as well to avoid races between different components.
	dbReply := db.Model(&common.Host{}).Where("id = ? and infra_env_id = ? and status = ?",
		hostId, infraEnvId, srcStatus).
		Updates(updates)

	if dbReply.Error != nil || (dbReply.RowsAffected == 0 && !hostExistsInDB(db, hostId, infraEnvId, updates)) {
		return nil, errors.Errorf("failed to update host %s from cluster %s. nothing has changed", hostId, infraEnvId)
	}

	var host *common.Host
	var err error

	if host, err = common.GetHostFromDB(db, infraEnvId.String(), hostId.String()); err != nil {
		return nil, errors.Wrapf(err, "failed to read from host %s from infraEnv %s from the database after the update", hostId, infraEnvId)
	}

	return host, nil
}

func hostExistsInDB(db *gorm.DB, hostId, infraEnvId strfmt.UUID, where map[string]interface{}) bool {
	where["id"] = hostId.String()
	where["infra_env_id"] = infraEnvId.String()
	var host models.Host
	return db.Select("id").Take(&host, where).Error == nil
}
