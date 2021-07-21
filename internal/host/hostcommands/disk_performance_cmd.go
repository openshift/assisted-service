package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type diskPerfCheckCmd struct {
	baseCmd
	hwValidator        hardware.Validator
	diskPerfCheckImage string
	timeoutSeconds     float64
}

func NewDiskPerfCheckCmd(log logrus.FieldLogger, diskPerfCheckImage string, hwValidator hardware.Validator, timeoutSeconds float64) *diskPerfCheckCmd {
	return &diskPerfCheckCmd{
		baseCmd:            baseCmd{log: log},
		diskPerfCheckImage: diskPerfCheckImage,
		hwValidator:        hwValidator,
		timeoutSeconds:     timeoutSeconds,
	}
}

func (c *diskPerfCheckCmd) GetSteps(_ context.Context, host *models.Host) ([]*models.Step, error) {
	bootDevice, err := hardware.GetBootDevice(c.hwValidator, host)
	if err != nil {
		return nil, err
	}
	alreadyExists, err := common.DiskSpeedResultExists(host.DisksInfo, bootDevice)
	if err != nil {
		return nil, err
	}

	if alreadyExists {
		return nil, nil
	}
	args, err := c.GetArgs(bootDevice)
	if err != nil {
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeInstallationDiskSpeedCheck,
		Command:  "bash",
		Args:     args,
	}
	return []*models.Step{step}, nil
}

func (c *diskPerfCheckCmd) GetArgs(bootDevice string) ([]string, error) {

	request := models.DiskSpeedCheckRequest{
		Path: swag.String(bootDevice),
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to marshal FioPerfCheckRequest")
		return nil, err
	}

	arguments := []string{
		"-c",
		"id=`podman ps --quiet --filter \"name=disk_performance\"` ; " +
			"test ! -z \"$id\" || " +
			fmt.Sprintf("timeout %f ", c.timeoutSeconds) +
			"podman run --privileged --rm --quiet -v /dev:/dev:rw -v /var/log:/var/log -v /run/systemd/journal/socket:/run/systemd/journal/socket " +
			"--name disk_performance " +
			c.diskPerfCheckImage + " disk_speed_check '" +
			string(requestBytes) + "'",
	}

	return arguments, nil
}
