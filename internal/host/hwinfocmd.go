package host

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type hwInfoCmd struct {
	baseCmd
	hardwareInfoImage string
}

func NewHwInfoCmd(log logrus.FieldLogger, hardwareInfoImage string) *hwInfoCmd {
	return &hwInfoCmd{
		baseCmd:           baseCmd{log: log},
		hardwareInfoImage: hardwareInfoImage,
	}
}

func (h *hwInfoCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeHardwareInfo
	step.Command = "podman"
	step.Args = strings.Split(fmt.Sprintf("run,--rm,--privileged,--quiet,--net=host,-v,/var/log:/var/log,%s,/usr/bin/hardware_info", h.hardwareInfoImage), ",")
	return step, nil
}
