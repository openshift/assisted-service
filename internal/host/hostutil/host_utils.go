package hostutil

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/coreos/ignition/v2/config/v3_2"
	ignition_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/common/ignition"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	MaxHostnameLength = 63
	HostnamePattern   = "^[a-z0-9][a-z0-9-]{0,62}(?:[.][a-z0-9-]{1,63})*$"

	StatusInfoMaxLength = 2040
)

var ForbiddenHostnames = []string{
	"localhost",
	"localhost.localdomain",
	"localhost4",
	"localhost4.localdomain4",
	"localhost6",
	"localhost6.localdomain6",
}

func GetCurrentHostName(host *models.Host) (string, error) {
	var inventory models.Inventory
	if host.RequestedHostname != "" {
		return host.RequestedHostname, nil
	}
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		return "", err
	}
	return inventory.Hostname, nil
}

func GetHostnameForMsg(host *models.Host) string {
	hostName, err := GetCurrentHostName(host)
	// An error here probably indicates that the agent didn't send inventory yet, fall back to UUID
	if err != nil || hostName == "" {
		return host.ID.String()
	}
	return hostName
}

func GetEventSeverityFromHostStatus(status string) string {
	switch status {
	case models.HostStatusDisconnected:
		return models.EventSeverityWarning
	case models.HostStatusInstallingPendingUserAction:
		return models.EventSeverityWarning
	case models.HostStatusInsufficient:
		return models.EventSeverityWarning
	case models.HostStatusError:
		return models.EventSeverityError
	default:
		return models.EventSeverityInfo
	}
}

func ValidateHostname(hostname string) error {
	if len(hostname) > MaxHostnameLength {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("hostname is too long, must be 63 characters or less. Hostname: %s has %d characters", hostname, len(hostname)))
	}
	b, err := regexp.MatchString(HostnamePattern, hostname)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Matching hostname"))
	}
	if !b {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Hostname does not pass required regex validation: %s. Hostname: %s", HostnamePattern, hostname))
	}

	if funk.ContainsString(ForbiddenHostnames, hostname) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("The host name %s is forbidden", hostname))
	}

	return nil
}

func IsRoleValid(requestedRole models.HostRole, isDay2Host bool) bool {
	roleSet := map[models.HostRole]struct{}{models.HostRoleAutoAssign: {}, models.HostRoleBootstrap: {}, models.HostRoleMaster: {}, models.HostRoleArbiter: {}, models.HostRoleWorker: {}}
	if isDay2Host {
		roleSet = map[models.HostRole]struct{}{models.HostRoleAutoAssign: {}, models.HostRoleMaster: {}, models.HostRoleArbiter: {}, models.HostRoleWorker: {}}
	}

	_, exists := roleSet[requestedRole]
	return exists
}

// determineDefaultInstallationDisk considers both the previously set installation disk and the current list of valid
// disks to determine the current required installation disk.
//
// Once the installation disk has been set, we usually no longer change it, even when an inventory update occurs
// that contains new disks that might be better "fit" for installation. This is because this field can also be set by
// the user via the API, and we don't want inventory updates to override the user's choice. However, if the disk that
// was set is no longer part of the inventory, the new installation disk is re-evaluated because it is not longer
// a valid choice.
func DetermineInstallationDisk(disks []*models.Disk, installationPath string) *models.Disk {
	if len(disks) == 0 {
		return nil
	}

	installationDisk := GetDiskByInstallationPath(disks, installationPath)

	if installationDisk == nil {
		installationDisk = disks[0]
	}

	return installationDisk
}

func GetHostInstallationPath(host *models.Host) string {
	if host.InstallationDiskID != "" {
		return host.InstallationDiskID
	}

	return host.InstallationDiskPath
}

func GetHostInstallationDisk(host *models.Host) (*models.Disk, error) {
	inventory, err := common.UnmarshalInventory(host.Inventory)

	if err != nil {
		return nil, err
	}

	installationDisk := GetDiskByInstallationPath(inventory.Disks, GetHostInstallationPath(host))
	if installationDisk == nil {
		return nil, fmt.Errorf("installation disk not found for host %s", host.ID)
	}
	return installationDisk, nil
}

func GetDiskByInstallationPath(disks []*models.Disk, installationPath string) *models.Disk {
	if len(disks) == 0 || installationPath == "" {
		return nil
	}

	// We changed the host.installationDiskPath to contain the disk id instead of the disk path.
	// We will search for the installation path in the disk.Id and the disk.Path field for backward compatibility.
	result := funk.Find(disks, func(disk *models.Disk) bool {
		return disk.ID == installationPath || common.GetDeviceFullName(disk) == installationPath
	})

	if result == nil {
		return nil
	}
	return result.(*models.Disk)
}

// GetAcceptableDisksWithHints finds acceptable installation disks based on the RootDeviceHints
//
// This function implements the logic to find the installation disk following what's currently
// supported by OpenShift, instead of *all* the supported cases in Ironic. The following link
// points to the RootDeviceDisk translation done by the BareMetal Operator that is then sent to
// Ironic:
// https://github.com/metal3-io/baremetal-operator/blob/dbe8780ad14f53132ba606d1baec808997febe49/pkg/provisioner/ironic/devicehints/devicehints.go#L11-L54
//
// The logic is quite straightforward and the checks done match what is in the aforementioned link.
// Some string checks require equality, others partial equality, whereas the int checks require numeric comparison.
//
// Ironic's internal filter process requires that all the disks have to fully match the RootDeviceHints (and operation),
// which is what this function does.
//
// This function also filters out disks that are not elegible for installation, as we already know those cannot be used.
func GetAcceptableDisksWithHints(disks []*models.Disk, hints *bmh_v1alpha1.RootDeviceHints) []*models.Disk {
	acceptable := []*models.Disk{}

	for _, disk := range disks {

		if !disk.InstallationEligibility.Eligible {
			continue
		}

		if hints != nil {
			if hints.DeviceName != "" && hints.DeviceName != disk.Path && hints.DeviceName != disk.ByPath {
				continue
			}

			if hints.HCTL != "" && hints.HCTL != disk.Hctl {
				continue
			}

			if hints.Model != "" && !strings.Contains(disk.Model, hints.Model) {
				continue
			}

			if hints.Vendor != "" && !strings.Contains(disk.Vendor, hints.Vendor) {
				continue
			}

			if hints.SerialNumber != "" && hints.SerialNumber != disk.Serial {
				continue
			}

			if hints.MinSizeGigabytes != 0 {
				sizeGB := int(conversions.BytesToGb(disk.SizeBytes))
				if sizeGB < hints.MinSizeGigabytes {
					continue
				}
			}

			if hints.WWN != "" && hints.WWN != disk.Wwn {
				continue
			}

			// No WWNWithExtension
			// if hints.WWWithExtension != "" && hints.WWWithExtension != disk.Wwwithextension {
			// 	return ""
			// }

			// No WWNNVendorExtension
			// if hints.WWNVendorExtension != "" && hints.WWNVendorExtension != disk.WwnVendorextension {
			// 	return ""
			// }

			if hints.Rotational != nil && *hints.Rotational != (disk.DriveType == models.DriveTypeHDD) {
				continue
			}
		}

		acceptable = append(acceptable, disk)
	}

	return acceptable
}

func IgnitionFileName(host *models.Host) string {
	return fmt.Sprintf("%s-%s.ign", common.GetEffectiveRole(host), host.ID)
}

func IsDay2Host(h *models.Host) bool {
	return swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost
}

func IsUnboundHost(h *models.Host) bool {
	return h.ClusterID == nil
}

func MarshalConnectivityReport(report *models.ConnectivityReport) (string, error) {
	if data, err := json.Marshal(report); err != nil {
		return "", err
	} else {
		return string(data), nil
	}
}

func UnmarshalConnectivityReport(reportStr string) (*models.ConnectivityReport, error) {
	var report models.ConnectivityReport

	if err := json.Unmarshal([]byte(reportStr), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func GetHostCluster(log logrus.FieldLogger, db *gorm.DB, host *models.Host) (*common.Cluster, error) {
	var cluster common.Cluster
	err := db.First(&cluster, "id = ?", host.ClusterID).Error
	if err != nil {
		log.WithError(err).Errorf("Failed to find cluster %s", host.ClusterID)
		return nil, errors.Errorf("Failed to find cluster %s", host.ClusterID)
	}
	return &cluster, nil
}

func IsSingleNode(log logrus.FieldLogger, db *gorm.DB, host *models.Host) bool {
	cluster, err := GetHostCluster(log, db, host)
	if err != nil {
		log.Debug("Can't check if host is part of single node OpenShift")
		return false
	}
	return common.IsSingleNodeCluster(cluster)
}

func SaveDiskPartitionsIsSet(installerArgs string) bool {
	needToSaveFlags := []string{"--save-partlabel", "--save-partindex"}
	for _, val := range needToSaveFlags {
		if strings.Contains(installerArgs, val) {
			return true
		}
	}
	return false
}

func IsDiskEncryptionEnabledForRole(encryption models.DiskEncryption, role models.HostRole) bool {
	if swag.StringValue(encryption.EnableOn) == models.DiskEncryptionEnableOnAll {
		return true
	}

	enabledGroups := strings.Split(swag.StringValue(encryption.EnableOn), ",")
	if role == models.HostRoleMaster || role == models.HostRoleBootstrap {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnMasters)
	}
	if role == models.HostRoleArbiter {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnArbiters)
	}
	if role == models.HostRoleWorker {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnWorkers)
	}
	return false
}

func GetDiskEncryptionForDay2(log logrus.FieldLogger, host *models.Host) (*ignition_types.Luks, error) {
	var response models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(host.APIVipConnectivity), &response); err != nil {
		// APIVipConnectivityResponse is not available yet - retrying.
		return nil, err
	}

	// Parse ignition from APIVipConnectivity (LUKS is supported in version >= 3.2)
	config, _, err := v3_2.Parse([]byte(response.Ignition))
	if err != nil {
		log.WithError(err).Warn("Ignition is empty or invalid - can't get disk encryption")
		return nil, err
	}

	// Checks if LUKS (disk encryption) exists
	if len(config.Storage.Luks) == 0 {
		// Disk encryption is disabled
		return nil, nil
	}

	// Return LUKS object
	return &config.Storage.Luks[0], nil
}

func GetIgnitionEndpointAndCert(cluster *common.Cluster, host *models.Host, logger logrus.FieldLogger) (string, *string, error) {
	poolName := string(common.GetEffectiveRole(host))

	// At this moment the effective role should already be either master or worker. However, given that
	// the default role is auto-assign, we want to explicitly handle this scenario. It should not happen
	// but if there is a bug elsewhere, we need to be on the safe side.
	if poolName == string(models.HostRoleAutoAssign) {
		poolName = string(models.HostRoleWorker)
	}

	if host.MachineConfigPoolName != "" {
		poolName = host.MachineConfigPoolName
	}

	// Determine the certificate to use
	// Priority order for certificate:
	// 1. Cluster-level certificate (if provided)
	// 2. Host-level certificate from ignition config overrides
	protocol := "http"
	port := constants.InsecureMCSPort
	var cert *string
	var err error
	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.CaCertificate != nil {
		logger.Infof("Using cluster ignition certificate for cluster %s, host %s", cluster.ID, host.ID)
		cert = cluster.IgnitionEndpoint.CaCertificate
		protocol = "https"
		port = constants.SecureMCSPort
	} else {
		cert, err = ignition.GetCACertInIgnition(host.IgnitionConfigOverrides)
		if err != nil {
			logger.Errorf("Failed to get Ignition certificate for host %s: %s", host.ID, err)
			return "", nil, err
		}
		if cert != nil {
			logger.Infof("Using host ignition certificate for cluster %s, host %s", cluster.ID, host.ID)
			protocol = "https"
			port = constants.SecureMCSPort
		} else {
			logger.Infof("No ignition certificate found for cluster %s, host %s; using HTTP", cluster.ID, host.ID)
		}
	}

	// Use custom ignition endpoint if provided
	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.URL != nil {
		url, err := url.Parse(*cluster.IgnitionEndpoint.URL)
		if err != nil {
			return "", nil, err
		}
		url.Path = path.Join(url.Path, poolName)
		logger.Infof("Using custom ignition endpoint for cluster %s, host %s: %s",
			cluster.ID, host.ID, url.String())
		return url.String(), cert, nil
	}

	// Determine the hostname
	// MCS certificates only include "api-int.<cluster>.<domain>" in their SANs,
	// not "api.<cluster>.<domain>", so for https, we must use api-int.
	apiVipDNSName := common.GetAPIHostname(cluster)
	apiHostname := apiVipDNSName

	// When using HTTPS with a certificate, check if we need to use api-int
	// Check if the API hostname looks like a DNS name (api.cluster.domain)
	// and convert it to internal endpoint (api-int.cluster.domain)
	// Replace "api." with "api-int." for internal MCS endpoint
	if cert != nil && strings.HasPrefix(apiVipDNSName, "api.") {
		apiHostname = strings.Replace(apiVipDNSName, "api.", fmt.Sprintf("%s.", constants.InternalAPIClusterSubdomain), 1)
		logger.Infof("Using internal API endpoint for HTTPS MCS connection: %s", apiHostname)
	}
	ignitionEndpointUrl := fmt.Sprintf(
		"%s://%s/config/%s",
		protocol,
		net.JoinHostPort(apiHostname, fmt.Sprint(port)),
		poolName)

	return ignitionEndpointUrl, cert, nil
}

func GetDisksOfHolderByType(allDisks []*models.Disk, holderDisk *models.Disk, driveTypeFilter models.DriveType) []*models.Disk {
	disksOfHolder := GetAllDisksOfHolder(allDisks, holderDisk)
	return lo.Filter(disksOfHolder, func(d *models.Disk, _ int) bool { return d.DriveType == driveTypeFilter })
}

func GetAllDisksOfHolder(allDisks []*models.Disk, holderDisk *models.Disk) []*models.Disk {
	disksOfHolder := []*models.Disk{}
	for _, disk := range allDisks {
		if strings.Contains(disk.Holders, holderDisk.Name) {
			disksOfHolder = append(disksOfHolder, disk)
		}
	}
	return disksOfHolder
}
