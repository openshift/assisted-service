package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/models"
)

type timestampCheckCmd struct {
	baseCmd
}

func NewTimestampCheckCmd(log logrus.FieldLogger) *timestampCheckCmd {
	return &timestampCheckCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (c *timestampCheckCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {

	step := &models.Step{
		StepType: models.StepTypeTimestamp,
		Command:  "date",
		Args: []string{
			"+%s",
		},
	}
	return step, nil
}
