package common

import (
	"encoding/json"

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
	info, ok := disksInfo[path]
	if !ok {
		info.Path = path
	}
	if exitCode == 0 || info.DiskSpeed == nil || !info.DiskSpeed.Tested {
		info.DiskSpeed = &models.DiskSpeed{
			ExitCode: exitCode,
			Tested:   true,
		}
	}
	if exitCode == 0 {
		info.DiskSpeed.SpeedMs = speedMs
	}
	disksInfo[path] = info
	return MarshalDisks(disksInfo)
}
