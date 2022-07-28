package common

import (
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/models"
)

type DisksInfo map[string]models.DiskInfo

func UnMarshalDisks(diskInfoStr string) (DisksInfo, error) {
	disksInfo := make(DisksInfo)
	if diskInfoStr == "" {
		return disksInfo, nil
	}
	if err := json.Unmarshal([]byte(diskInfoStr), &disksInfo); err != nil {
		return nil, err
	}
	return disksInfo, nil
}

func MarshalDisks(disksInfo DisksInfo) (string, error) {
	b, err := json.Marshal(&disksInfo)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func DiskSpeedResultExists(disksInfoStr, path string) (bool, error) {
	disksInfo, err := UnMarshalDisks(disksInfoStr)
	if err != nil {
		return false, err
	}
	info, ok := disksInfo[path]
	return ok && info.DiskSpeed != nil && info.DiskSpeed.Tested, nil
}

func GetDiskInfo(disksInfoStr, path string) (*models.DiskInfo, error) {
	disksInfo, err := UnMarshalDisks(disksInfoStr)
	if err != nil {
		return nil, err
	}
	if info, ok := disksInfo[path]; ok {
		return &info, nil
	}
	return nil, nil
}

func SetDiskSpeed(path string, speedMs int64, exitCode int64, disksInfoStr string) (string, error) {
	disksInfo, err := UnMarshalDisks(disksInfoStr)
	if err != nil {
		return "", err
	}
	info := disksInfo[path]
	info.Path = path
	info.DiskSpeed = &models.DiskSpeed{
		ExitCode: exitCode,
		Tested:   true,
	}
	if exitCode == 0 {
		info.DiskSpeed.SpeedMs = speedMs
	}
	disksInfo[path] = info
	return MarshalDisks(disksInfo)
}

func ResetDiskSpeed(path, disksInfoStr string) (string, error) {
	disksInfo, err := UnMarshalDisks(disksInfoStr)
	if err != nil {
		return "", err
	}
	info, ok := disksInfo[path]
	if !ok {
		return disksInfoStr, nil
	}
	info.DiskSpeed = nil
	disksInfo[path] = info
	return MarshalDisks(disksInfo)
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
