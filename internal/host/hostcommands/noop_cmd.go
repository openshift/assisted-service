package hostcommands

import (
	"context"

	models "github.com/openshift/assisted-service/models/v1"
)

func NewNoopCmd() CommandGetter {
	return &noopCmd{}
}

type noopCmd struct{}

func (n *noopCmd) GetSteps(_ context.Context, _ *models.Host) ([]*models.Step, error) {
	return nil, nil
}
