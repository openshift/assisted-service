package host

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type formatDiskCmd struct {
	baseCmd
}

func NewFormatDiskCmd(log logrus.FieldLogger) *formatDiskCmd {
	return &formatDiskCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (d *formatDiskCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {

	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	var steps []*models.Step
	for _, disk := range inventory.Disks {
		if disk.Bootable {
			step := &models.Step{}
			step.StepType = models.StepTypeExecute
			step.Command = "dd"
			of := fmt.Sprintf("of=/dev/%s", disk.Name)
			step.Args = []string{"if=/dev/zero", of, "bs=10M", "count=1"}
			steps = append(steps, step)
		}
	}

	return steps, nil
}
