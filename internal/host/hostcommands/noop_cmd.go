package hostcommands

import (
	"context"

	"github.com/openshift/assisted-service/models"
)

func NewNoopCmd() CommandGetter {
	return &noopCmd{}
}

type noopCmd struct{}

func (n *noopCmd) GetSteps(_ context.Context, _ *models.Host) ([]*models.Step, error) {
	return nil, nil
}
