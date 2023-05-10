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

	"github.com/go-openapi/swag"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	MaxHostnameLength = 64
	HostnamePattern   = "^[a-z0-9][a-z0-9-]{0,62}(?:[.][a-z0-9-]{1,63})*$"
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

func IsBeforeInstallation(status string) bool {
	beforeInstallation := []string{models.HostStatusDiscovering, models.HostStatusKnown,
		models.HostStatusDisconnected, models.HostStatusInsufficient,
		models.HostStatusPendingForInput, models.HostStatusBinding}
	return funk.ContainsString(beforeInstallation, status)
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
		return common.NewApiError(http.StatusBadRequest, errors.New("hostname is too long"))
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
	roleSet := map[models.HostRole]struct{}{models.HostRoleAutoAssign: {}, models.HostRoleBootstrap: {}, models.HostRoleMaster: {}, models.HostRoleWorker: {}}
	if isDay2Host {
		roleSet = map[models.HostRole]struct{}{models.HostRoleMaster: {}, models.HostRoleWorker: {}}
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

	return GetDiskByInstallationPath(inventory.Disks, GetHostInstallationPath(host)), nil
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
			if hints.DeviceName != "" && hints.DeviceName != disk.Path {
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
	switch swag.StringValue(encryption.EnableOn) {
	case models.DiskEncryptionEnableOnAll:
		return true
	case models.DiskEncryptionEnableOnMasters:
		return role == models.HostRoleMaster || role == models.HostRoleBootstrap
	case models.DiskEncryptionEnableOnWorkers:
		return role == models.HostRoleWorker
	default:
		return false
	}
}

func GetIgnitionEndpoint(cluster *common.Cluster, host *models.Host) (string, error) {
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

	ignitionEndpointUrl := fmt.Sprintf(
		"http://%s/config/%s",
		net.JoinHostPort(common.GetAPIHostname(cluster), fmt.Sprint(constants.InsecureMCSPort)),
		poolName)
	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.URL != nil {
		url, err := url.Parse(*cluster.IgnitionEndpoint.URL)
		if err != nil {
			return "", err
		}
		url.Path = path.Join(url.Path, poolName)
		ignitionEndpointUrl = url.String()
	}
	return ignitionEndpointUrl, nil
}
