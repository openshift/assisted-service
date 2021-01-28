package host

import (
	"net/http"
	"time"

	"github.com/openshift/assisted-service/internal/host/hostutil"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/thoas/go-funk"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	statusInfoDisconnected                                     = "Host has stopped communicating with the installation service"
	statusInfoDisabled                                         = "Host was manually disabled"
	statusInfoDiscovering                                      = "Waiting for host to send hardware details"
	statusInfoInsufficientHardware                             = "Host does not meet the minimum hardware requirements: $FAILING_VALIDATIONS"
	statusInfoPendingForInput                                  = "Waiting for user input: $FAILING_VALIDATIONS"
	statusInfoNotReadyForInstall                               = "Host cannot be installed due to following failing validation(s): $FAILING_VALIDATIONS"
	statusInfoKnown                                            = "Host is ready to be installed"
	statusInfoInstalling                                       = "Installation is in progress"
	statusInfoResettingPendingUserAction                       = "Host requires booting into the discovery image to complete resetting the installation"
	statusInfoPreparingForInstallation                         = "Host is preparing for installation"
	statusInfoPreparingTimedOut                                = "Host failed to install because its preparation took longer than expected"
	statusInfoAbortingDueClusterErrors                         = "Host is part of a cluster that failed to install"
	statusInfoInstallationTimedOut                             = "Host failed to install due to timeout while starting installation"
	statusInfoConnectionTimedOut                               = "Host failed to install due to timeout while connecting to host"
	statusInfoInstallationInProgressTimedOut                   = "Host failed to install because its installation stage $STAGE took longer than expected $MAX_TIME"
	statusInfoInstallationInProgressWritingImageToDiskTimedOut = "Host failed to install because its installation stage $STAGE did not sufficiently progress in the last $MAX_TIME."
)

var hostStatusesBeforeInstallation = [...]string{
	models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
	models.HostStatusInsufficient, models.HostStatusPendingForInput,
}

type UpdateReply struct {
	State     string
	IsChanged bool
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
	if host, err = hostutil.UpdateHost(log, db, clusterId, hostId, srcStatus, "progress_stage_updated_at", now); err != nil {
		return nil, errors.Wrapf(
			err,
			"failed to refresh host status update time %s from cluster %s state from %s",
			hostId, clusterId, srcStatus)
	}

	return host, nil
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
func updateRole(log logrus.FieldLogger, h *models.Host, role models.HostRole, db *gorm.DB, srcRole *string) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallation[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host role can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	extras := append(make([]interface{}, 0), "role", role)

	if hostutil.IsDay2Host(h) && (h.MachineConfigPoolName == "" || h.MachineConfigPoolName == *srcRole) {
		extras = append(extras, "machine_config_pool_name", role)
	}

	_, err := hostutil.UpdateHost(log, db, h.ClusterID, *h.ID, *h.Status, extras...)
	return err
}
