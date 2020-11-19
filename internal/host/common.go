package host

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openshift/assisted-service/internal/hostutil"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/thoas/go-funk"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	statusInfoDisconnected                   = "Host has stopped communicating with the installation service"
	statusInfoDisabled                       = "Host was manually disabled"
	statusInfoDiscovering                    = "Waiting for host to send hardware details"
	statusInfoInsufficientHardware           = "Host does not meet the minimum hardware requirements: $FAILING_VALIDATIONS"
	statusInfoPendingForInput                = "Waiting for user input: $FAILING_VALIDATIONS"
	statusInfoNotReadyForInstall             = "Host cannot be installed due to following failing validation(s): $FAILING_VALIDATIONS"
	statusInfoKnown                          = "Host is ready to be installed"
	statusInfoInstalling                     = "Installation is in progress"
	statusInfoResettingPendingUserAction     = "Host requires booting into the discovery image to complete resetting the installation"
	statusInfoPreparingForInstallation       = "Host is preparing for installation"
	statusInfoPreparingTimedOut              = "Host failed to install because its preparation took longer than expected"
	statusInfoAbortingDueClusterErrors       = "Host is part of a cluster that failed to install"
	statusInfoInstallationTimedOut           = "Host failed to install due to timeout while starting installation"
	statusInfoInstallationInProgressTimedOut = "Host failed to install because its installation stage $STAGE took longer than expected $MAX_TIME"
	hostNotRespondingNotification            = ", Host is not responding, last respond was at "
)

type UpdateReply struct {
	State     string
	IsChanged bool
}

func updateHostProgress(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, clusterId strfmt.UUID, hostId strfmt.UUID,
	srcStatus string, newStatus string, statusInfo string,
	srcStage models.HostStage, newStage models.HostStage, progressInfo string, extra ...interface{}) (*models.Host, error) {

	extra = append(append(make([]interface{}, 0), "progress_current_stage", newStage, "progress_progress_info", progressInfo,
		"progress_stage_updated_at", strfmt.DateTime(time.Now())), extra...)

	if newStage != srcStage {
		extra = append(extra, "progress_stage_started_at", strfmt.DateTime(time.Now()))
	}

	return updateHostStatus(ctx, log, db, eventsHandler, clusterId, hostId, srcStatus, newStatus, statusInfo, extra...)
}

func updateHostStatus(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, clusterId strfmt.UUID, hostId strfmt.UUID,
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

	if newStatus != srcStatus {
		msg := fmt.Sprintf("Host %s: updated status from \"%s\" to \"%s\"", hostutil.GetHostnameForMsg(host), srcStatus, newStatus)
		if statusInfo != "" {
			msg += fmt.Sprintf(" (%s)", statusInfo)
		}
		eventsHandler.AddEvent(ctx, clusterId, &hostId, hostutil.GetEventSeverityFromHostStatus(newStatus), msg, time.Now())
		log.Infof("host %s from cluster %s has been updated with the following updates %+v", hostId, clusterId, extra)
	}

	return host, nil
}

func refreshHostStageUpdateTime(
	log logrus.FieldLogger,
	db *gorm.DB,
	clusterId strfmt.UUID,
	hostId strfmt.UUID,
	srcStatus string) (*models.Host, error) {
	var host *models.Host
	var err error

	now := strfmt.DateTime(time.Now())
	if host, err = UpdateHost(log, db, clusterId, hostId, srcStatus, "progress_stage_updated_at", now); err != nil {
		return nil, errors.Wrapf(
			err,
			"failed to refresh host status update time %s from cluster %s state from %s",
			hostId, clusterId, srcStatus)
	}

	return host, nil
}

func hostExistsInDB(db *gorm.DB, hostId, clusterId strfmt.UUID, where map[string]interface{}) bool {
	where["id"] = hostId.String()
	where["cluster_id"] = clusterId.String()
	var host models.Host
	return db.Select("id").Take(&host, where).Error == nil
}

func isDay2Host(h *models.Host) bool {
	day2HostKinds := []string{models.HostKindAddToExistingClusterHost,
		models.HostKindAddToExistingClusterOCPHost}
	return funk.ContainsString(day2HostKinds, swag.StringValue(h.Kind))
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
		return nil, errors.Errorf("failed to update host %s from cluster %s. nothing has changed", hostId, clusterId)
	}

	var host models.Host

	if err := db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error; err != nil {
		return nil, errors.Wrapf(err, "failed to read from host %s from cluster %s from the database after the update",
			hostId, clusterId)
	}

	return &host, nil
}

func indexOfStage(element models.HostStage, data []models.HostStage) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1 // not found.
}

// update host role with an option to update only if the current role is srcRole to prevent races
func updateRole(h *models.Host, role models.HostRole, db *gorm.DB, srcRole *string) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := []string{
		models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
		models.HostStatusInsufficient, models.HostStatusPendingForInput,
	}
	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host role can be set only in one of %s states",
				hostStatus, allowedStatuses))
	}

	h.Role = role
	update := db.Model(h)
	if srcRole != nil {
		update = update.Where("role = ?", swag.StringValue(srcRole))
	}
	updateReply := update.Update("role", role)
	if updateReply.RowsAffected == 0 {
		return errors.Errorf("failed to update host %s from cluster %s role to %s - nothing changed",
			h.ID.String(), h.ClusterID.String(), role)
	}
	if updateReply.Error != nil {
		return errors.Wrapf(updateReply.Error, "failed to update host %s from cluster %s role to %s",
			h.ID.String(), h.ClusterID.String(), role)
	}
	return nil
}

func CreateUploadLogsCmd(host *models.Host, baseURL, agentImage, mastersIPs string, skipCertVerification, preservePreviousCommandReturnCode,
	withInstallerGatherLogging bool) (string, error) {

	cmdArgsTmpl := ""
	if preservePreviousCommandReturnCode {
		cmdArgsTmpl = "( returnCode=$?; "
	}

	data := map[string]string{
		"BASE_URL":               strings.TrimSpace(baseURL),
		"CLUSTER_ID":             string(host.ClusterID),
		"HOST_ID":                string(*host.ID),
		"AGENT_IMAGE":            strings.TrimSpace(agentImage),
		"SKIP_CERT_VERIFICATION": strconv.FormatBool(skipCertVerification),
		"BOOTSTRAP":              strconv.FormatBool(host.Bootstrap),
		"INSTALLER_GATHER":       strconv.FormatBool(withInstallerGatherLogging),
		"MASTERS_IPS":            mastersIPs,
	}
	cmdArgsTmpl += "podman run --rm --privileged " +
		"-v /run/systemd/journal/socket:/run/systemd/journal/socket -v /var/log:/var/log " +
		"--env PULL_SECRET_TOKEN --name logs-sender --pid=host {{.AGENT_IMAGE}} logs_sender " +
		"-url {{.BASE_URL}} -cluster-id {{.CLUSTER_ID}} -host-id {{.HOST_ID}} " +
		"--insecure={{.SKIP_CERT_VERIFICATION}} -bootstrap={{.BOOTSTRAP}} -with-installer-gather-logging={{.INSTALLER_GATHER}}" +
		"{{if .MASTERS_IPS}} -masters-ips={{.MASTERS_IPS}} {{end}}"

	if preservePreviousCommandReturnCode {
		cmdArgsTmpl = cmdArgsTmpl + "; exit $returnCode; )"
	}
	t, err := template.New("cmd").Parse(cmdArgsTmpl)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
