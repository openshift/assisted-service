package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type rebootForReclaimCmd struct {
	baseCmd
	HostFSMountDir string
}

func NewRebootForReclaimCmd(log logrus.FieldLogger, hostFSMountDir string) *rebootForReclaimCmd {
	return &rebootForReclaimCmd{
		baseCmd:        baseCmd{log: log},
		HostFSMountDir: hostFSMountDir,
	}
}

func (c *rebootForReclaimCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	request := models.RebootForReclaimRequest{
		HostFsMountDir: &c.HostFSMountDir,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RebootForReclaimRequest: %w", err)
	}
	step := &models.Step{
		StepType: models.StepTypeRebootForReclaim,
		Args: []string{
			string(requestBytes),
		},
	}
	return []*models.Step{step}, nil
}
