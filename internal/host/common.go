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
	statusInfoHostPreparationFailure                           = "Host failed to prepare for installation due to following failing validation(s): $FAILING_VALIDATIONS"
	statusInfoAbortingDueClusterErrors                         = "Host is part of a cluster that failed to install"
	statusInfoInstallationTimedOut                             = "Host failed to install due to timeout while starting installation"
	statusInfoConnectionTimedOut                               = "Host failed to install due to timeout while connecting to host"
	statusInfoInstallationInProgressTimedOut                   = "Host failed to install because its installation stage $STAGE took longer than expected $MAX_TIME"
	statusInfoInstallationInProgressWritingImageToDiskTimedOut = "Host failed to install because its installation stage $STAGE did not sufficiently progress in the last $MAX_TIME."
	statusInfoHostReadyToBeBound                               = "Host is ready to be bound to a cluster"
	statusInfoBinding                                          = "Host is waiting to be bound to the cluster"
	statusRebootTimeout                                        = "Host failed to reboot within timeout, please boot the host from the the OpenShift installation disk $INSTALLATION_DISK. The installation will resume once the host reboot"
	statusInfoUnbinding                                        = "Host is waiting to be unbound from the cluster"
	statusInfoRebootingDay2                                    = "Host has rebooted and no further updates will be posted. Please check console for progress and to possibly approve pending CSRs"

	nilInventoryErrorTemplate       = "inventory must not be nil"
	noInterfaceForNameErrorTemplate = "unable to find interface for name %s"
)

var hostStatusesBeforeInstallation = [...]string{
	models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
	models.HostStatusInsufficient, models.HostStatusPendingForInput,
}

var hostStatusesInInfraEnv = [...]string{
	models.HostStatusDisconnectedUnbound, models.HostStatusInsufficientUnbound, models.HostStatusDiscoveringUnbound,
	models.HostStatusKnownUnbound,
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
	allowedStatuses := append(hostStatusesBeforeInstallation[:], hostStatusesInInfraEnv[:]...)
	if !funk.ContainsString(allowedStatuses, hostStatus) {
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

func GetInterfaceByName(name string, inventory *models.Inventory) (*models.Interface, error) {
	if inventory == nil {
		return nil, fmt.Errorf(nilInventoryErrorTemplate)
	}
	for _, intf := range inventory.Interfaces {
		if intf.Name == name {
			return intf, nil
		}
	}
	return nil, fmt.Errorf(noInterfaceForNameErrorTemplate, name)
}

func GetInterfaceByIp(ip string, inventory *models.Inventory) (*models.Interface, error) {
	if inventory == nil {
		return nil, fmt.Errorf(nilInventoryErrorTemplate)
	}
	for _, intf := range inventory.Interfaces {
		for _, cidr := range intf.IPV4Addresses {
			parsedAddr, _, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, err
			}
			if parsedAddr.String() == ip {
				return intf, nil
			}
		}
		for _, cidr := range intf.IPV6Addresses {
			parsedAddr, _, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, err
			}
			if parsedAddr.Equal(net.ParseIP(ip)) {
				return intf, nil
			}
		}
	}
	return nil, fmt.Errorf("unable to find interface for ip %s", ip)
}

func GetHostByIP(ip string, hosts []*models.Host) (*models.Host, error) {
	if hosts == nil {
		return nil, fmt.Errorf(nilInventoryErrorTemplate)
	}
	for _, h := range hosts {
		if h.Inventory == "" {
			continue
		}
		inv, err := common.UnmarshalInventory(h.Inventory)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshall cluster inventory for host %s: %s", h.RequestedHostname, err)
		}
		for _, i := range inv.Interfaces {
			ips := append(i.IPV4Addresses, i.IPV6Addresses...)
			for _, cidr := range ips {
				parsedIP, _, err := net.ParseCIDR(cidr)
				if err != nil {
					return nil, err
				}
				if parsedIP.Equal(net.ParseIP(ip)) {
					return h, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("host with IP %s not found in inventory", ip)
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
				if parsedIP.Equal(net.ParseIP(ip)) {
					return getRealHostname(h, inv), common.GetEffectiveRole(h), nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("host with IP %s not found in inventory", ip)
}
