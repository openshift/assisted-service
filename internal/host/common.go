package host

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
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
	statusInfoHostPreparationSuccessful                        = "Host finished successfully to prepare for installation"
	statusInfoAbortingDueClusterErrors                         = "Host is part of a cluster that failed to install"
	statusInfoInstallationTimedOut                             = "Host failed to install due to timeout while starting installation"
	statusInfoConnectionTimedOut                               = "Host failed to install due to timeout while connecting to host"
	statusInfoInstallationInProgressTimedOut                   = "Host failed to install because its installation stage $STAGE took longer than expected $MAX_TIME"
	statusInfoInstallationInProgressWritingImageToDiskTimedOut = "Host failed to install because its installation stage $STAGE did not sufficiently progress in the last $MAX_TIME."
	statusInfoHostReadyToBeMoved                               = "Host is part of pool and is ready to be moved"
	statusInfoBinding                                          = "Host is waiting tobe bound to the cluster"
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
	srcStatus string) (*common.Host, error) {
	var host *common.Host
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
	extras = append(extras, "trigger_monitor_timestamp", time.Now())

	_, err := hostutil.UpdateHost(log, db, *h.ClusterID, *h.ID, *h.Status, extras...)
	return err
}

func GetHostnameAndRoleByIP(ip string, hosts []*models.Host) (string, models.HostRole, error) {
	for _, h := range hosts {
		if h.Inventory == "" {
			continue
		}
		inv, err := hostutil.UnmarshalInventory(h.Inventory)
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
					return getRealHostname(h, inv), h.Role, nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("host with IP %s not found in inventory", ip)
}
