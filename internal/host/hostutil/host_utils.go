package hostutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	MaxHostnameLength = 253
)

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
	pattern := "^[a-z0-9][a-z0-9-]{0,62}(?:[.][a-z0-9-]{1,63})*$"
	b, err := regexp.MatchString(pattern, hostname)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Matching hostname"))
	}
	if !b {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Hostname does not pass required regex validation: %s. Hostname: %s", pattern, hostname))
	}
	return nil
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

func GetDeviceIdentifier(installationDisk *models.Disk) string {
	// We changed the host.installationDiskPath to contain the disk id instead of the disk path.
	// Here we updates the old installationDiskPath to the disk id.
	// (That's the reason we return the disk.ID instead of the previousInstallationDisk if exist)
	if installationDisk.ID != "" {
		return installationDisk.ID
	}

	// Old inventory or a bug
	return GetDeviceFullName(installationDisk)
}

func GetDeviceFullName(installationDisk *models.Disk) string {
	return fmt.Sprintf("/dev/%s", installationDisk.Name)
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
		return disk.ID == installationPath || GetDeviceFullName(disk) == installationPath
	})

	if result == nil {
		return nil
	}
	return result.(*models.Disk)
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

	// Specify ignition endpoint based on cluster configuration:
	address := cluster.APIVip
	if address == "" {
		address = swag.StringValue(cluster.APIVipDNSName)
	}

	ignitionEndpointUrl := fmt.Sprintf("http://%s:22624/config/%s", address, poolName)
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
