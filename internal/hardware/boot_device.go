package hardware

import (
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

func GetBootDevice(hwValidator Validator, host *models.Host) (string, error) {
	path := hwValidator.GetHostInstallationPath(host)

	if path != "" {
		return path, nil
	}

	return "", errors.Errorf("host has no installation path %s", host.ID)
}
