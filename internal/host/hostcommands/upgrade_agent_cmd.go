package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

type upgradeAgentCmd struct {
	baseCmd
	agentImage string
}

func NewUpgradeAgentCmd(agentImage string) *upgradeAgentCmd {
	return &upgradeAgentCmd{
		agentImage: agentImage,
	}
}

func (c *upgradeAgentCmd) GetSteps(ctx context.Context, host *models.Host) (result []*models.Step,
	err error) {
	// Prepare the parameters:
	request := models.UpgradeAgentRequest{
		AgentImage: c.agentImage,
	}
	data, err := json.Marshal(request)
	if err != nil {
		return
	}

	// Create the steps:
	result = []*models.Step{{
		StepType: models.StepTypeUpgradeAgent,
		Args: []string{
			string(data),
		},
	}}
	return
}
