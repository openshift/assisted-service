package hardware

import (
	"github.com/openshift/assisted-service/internal/host/hostutil"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func GetBootDevice(log logrus.FieldLogger, hwValidator Validator, host *models.Host) (string, error) {
	if host.InstallationDiskPath != "" {
		return host.InstallationDiskPath, nil
	}

	// TODO: We generally shouldn't reach this point on any version containing this comment.
	//  It might happen a few times while this version is first rolled out. Remove the call to GetHostValidDisks when
	//  that new version has been running for a sufficiently long time.
	//  Note that instead of a call to GetHostValidDisks, an error should occur. That's because if the installation disk
	//  path is empty, it means there are no valid disks to install on.
	disks, err := hwValidator.GetHostValidDisks(host)
	if err != nil || len(disks) == 0 {
		log.Errorf("Failed to get valid disks on host with id %s", host.ID)

		var newErr error
		if err != nil {
			newErr = errors.Wrapf(err, "failed to get valid disks on host with id %s", host.ID)
		} else {
			newErr = errors.Errorf("host has no valid disks id %s", host.ID)
		}

		return "", newErr
	}
	return hostutil.GetDeviceFullName(disks[0]), nil
}
