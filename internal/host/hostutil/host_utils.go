package hostutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
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

func MarshalInventory(inventory *models.Inventory) (string, error) {
	if data, err := json.Marshal(inventory); err != nil {
		return "", err
	} else {
		return string(data), nil
	}
}

func UnmarshalInventory(inventoryStr string) (*models.Inventory, error) {
	var inventory models.Inventory

	if err := json.Unmarshal([]byte(inventoryStr), &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}

func GetHostInstallationPath(host *models.Host) string {
	if host.InstallationDiskID != "" {
		return host.InstallationDiskID
	}

	return host.InstallationDiskPath
}

func GetHostInstallationDisk(host *models.Host) (*models.Disk, error) {
	inventory, err := UnmarshalInventory(host.Inventory)

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
	return fmt.Sprintf("%s-%s.ign", host.Role, host.ID)
}

var allowedFlags = []string{"--append-karg", "--delete-karg", "-n", "--copy-network", "--network-dir", "--save-partlabel", "--save-partindex", "--image-url"}

func ValidateInstallerArgs(args []string) error {
	re := regexp.MustCompile("^-+.*")
	for _, arg := range args {
		if !re.MatchString(arg) {
			continue
		}

		found := false
		for _, f := range allowedFlags {
			if arg == f {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("found unexpected flag %s for installer - allowed flags are %v", arg, allowedFlags)
		}
	}

	return nil
}

func IsDay2Host(h *models.Host) bool {
	return swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost
}

// GetAddressFamilies returns if a host has addresses in IPv4, in IPv6 family, or both
func GetAddressFamilies(host *models.Host) (bool, bool, error) {
	inventory, err := UnmarshalInventory(host.Inventory)
	if err != nil {
		return false, false, err
	}
	v4 := false
	v6 := false
	for _, i := range inventory.Interfaces {
		v4 = v4 || len(i.IPV4Addresses) > 0
		v6 = v6 || len(i.IPV6Addresses) > 0
		if v4 && v6 {
			break
		}
	}
	return v4, v6, nil
}
