package host

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type hwInfoCmd baseCmd

func NewHwInfoCmd(log logrus.FieldLogger) *hwInfoCmd {
	return &hwInfoCmd{
		log: log,
	}
}

func (h *hwInfoCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeHardwareInfo
	step.Command = "podman"
	step.Args = strings.Split("run,--rm,--privileged,--quiet,--net=host,-v,/var/log:/var/log,quay.io/oamizur/hardware_info,/usr/bin/hardware_info", ",")
	return step, nil
}
