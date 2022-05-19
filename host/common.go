package host

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	statusInfoMediaDisconnected                                = "Unable to read from the discovery media. It was either disconnected or poor network conditions prevented it from being read. Try using the minimal ISO option and be sure to keep the media connected until the installation is completed"
	statusInfoDisconnected                                     = "Host has stopped communicating with the installation service"
	statusInfoDiscovering                                      = "Waiting for host to send hardware details"
	statusInfoInsufficientHardware                             = "Host does not meet the minimum hardware requirements: $FAILING_VALIDATIONS"
	statusInfoPendingForInput                                  = "Waiting for user input: $FAILING_VALIDATIONS"
	statusInfoNotReadyForInstall                               = "Host cannot be installed due to following failing validation(s): $FAILING_VALIDATIONS"
	statusInfoKnown                                            = "Host is ready to be installed"
	statusInfoInstalling                                       = "Installation is in progress"
	statusInfoResettingPendingUserAction                       = "Host requires booting into the discovery image to complete resetting the installation"
	statusInfoPreparingForInstallation                         = "Host is preparing for installation"
	statusInfoHostPreparationSuccessful                        = "Host finished successfully to prepare for installation"
	statusInfoHostPreparationFailure                           = "Host failed to prepare for installation due to following failing validation(s): $FAILING_VALIDATIONS"
	statusInfoAbortingDueClusterErrors                         = "Host is part of a cluster that failed to install"
	statusInfoInstallationTimedOut                             = "Host failed to install due to timeout while starting installation"
	statusInfoConnectionTimedOut                               = "Host failed to install due to timeout while connecting to host"
	statusInfoInstallationInProgressTimedOut                   = "Host failed to install because its installation stage $STAGE took longer than expected $MAX_TIME"
	statusInfoInstallationInProgressWritingImageToDiskTimedOut = "Host failed to install because its installation stage $STAGE did not sufficiently progress in the last $MAX_TIME."
	statusInfoHostReadyToBeBound                               = "Host is ready to be bound to a cluster"
	statusInfoBinding                                          = "Host is waiting to be bound to the cluster"
	statusRebootTimeout                                        = "Host failed to reboot within timeout, please boot the host from the the OpenShift installation disk $INSTALLATION_DISK. The installation will resume once the host has rebooted"
	statusInfoUnbinding                                        = "Host is waiting to be unbound from the cluster"
	statusInfoRebootingDay2                                    = "Host has rebooted and no further updates will be posted. Please check console for progress and to possibly approve pending CSRs"
)

var BootstrapStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageWaitingForControlPlane,
	models.HostStageWaitingForBootkube, models.HostStageWaitingForController,
	models.HostStageRebooting, models.HostStageConfiguring, models.HostStageJoined,
	models.HostStageDone,
}
var MasterStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageRebooting,
	models.HostStageConfiguring, models.HostStageJoined, models.HostStageDone,
}
var WorkerStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageWaitingForControlPlane, models.HostStageRebooting,
	models.HostStageWaitingForIgnition, models.HostStageConfiguring,
	models.HostStageJoined, models.HostStageDone,
}
var SnoStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWaitingForBootkube, models.HostStageWritingImageToDisk,
	models.HostStageRebooting, models.HostStageDone,
}

var manualRebootStages = []models.HostStage{
	models.HostStageRebooting,
	models.HostStageWaitingForIgnition,
	models.HostStageConfiguring,
	models.HostStageJoined,
	models.HostStageDone,
}

var hostStatusesBeforeInstallation = [...]string{
	models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
	models.HostStatusInsufficient, models.HostStatusPendingForInput,
}

var hostStatusesInInfraEnv = [...]string{
	models.HostStatusDisconnectedUnbound, models.HostStatusInsufficientUnbound, models.HostStatusDiscoveringUnbound,
	models.HostStatusKnownUnbound,
}

var hostStatusesBeforeInstallationOrUnbound = [...]string{
	models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
	models.HostStatusInsufficient, models.HostStatusPendingForInput,
	models.HostStatusDisconnectedUnbound, models.HostStatusInsufficientUnbound, models.HostStatusDiscoveringUnbound,
	models.HostStatusKnownUnbound,
	models.HostStatusBinding,
}

type UpdateReply struct {
	State     string
	IsChanged bool
}

func refreshHostStageUpdateTime(
	log logrus.FieldLogger,
	db *gorm.DB,
	infraEnvId strfmt.UUID,
	hostId strfmt.UUID,
	srcStatus string) (*common.Host, error) {
	var host *common.Host
	var err error

	now := strfmt.DateTime(time.Now())
	if host, err = hostutil.UpdateHost(log, db, infraEnvId, hostId, srcStatus, "progress_stage_updated_at", now); err != nil {
		return nil, errors.Wrapf(
			err,
			"failed to refresh host status update time %s from infraEnv %s state from %s",
			hostId, infraEnvId, srcStatus)
	}

	return host, nil
}

// update host role with an option to update only if the current role is srcRole to prevent races
func updateRole(log logrus.FieldLogger, h *models.Host, role models.HostRole, suggestedRole models.HostRole, db *gorm.DB, srcRole string) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host role can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	fields := make(map[string]interface{})
	fields["suggested_role"] = suggestedRole
	if srcRole != string(role) {
		fields["role"] = role

		if hostutil.IsDay2Host(h) && (h.MachineConfigPoolName == "" || h.MachineConfigPoolName == srcRole) {
			fields["machine_config_pool_name"] = role
		}
	}
	fields["trigger_monitor_timestamp"] = time.Now()

	return db.Model(&common.Host{}).Where("id = ? and infra_env_id = ? and role = ?",
		*h.ID, h.InfraEnvID, srcRole).Updates(fields).Error
}

func GetHostnameAndEffectiveRoleByIP(ip string, hosts []*models.Host) (string, models.HostRole, error) {
	for _, h := range hosts {
		if h.Inventory == "" {
			continue
		}
		inv, err := common.UnmarshalInventory(h.Inventory)
		if err != nil {
			return "", "", fmt.Errorf("unable to unmarshall cluster inventory for host %s: %s", h.RequestedHostname, err)
		}
		for _, i := range inv.Interfaces {
			ips := append(i.IPV4Addresses, i.IPV6Addresses...)
			for _, cidr := range ips {
				parsedIP, _, err := net.ParseCIDR(cidr)
				if err != nil {
					return "", "", err
				}
				if ip == parsedIP.String() {
					return getRealHostname(h, inv), common.GetEffectiveRole(h), nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("host with IP %s not found in inventory", ip)
}

func FindMatchingStages(role models.HostRole, bootstrap, isSNO bool) []models.HostStage {
	var stages []models.HostStage
	switch {
	case bootstrap || role == models.HostRoleBootstrap:
		if isSNO {
			stages = SnoStages[:]
		} else {
			stages = BootstrapStages[:]
		}
	case role == models.HostRoleMaster:
		stages = MasterStages[:]
	case role == models.HostRoleWorker:
		stages = WorkerStages[:]
	default:
		stages = []models.HostStage{}
	}

	return stages
}
