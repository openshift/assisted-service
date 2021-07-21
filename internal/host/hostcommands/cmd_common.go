package hostcommands

import (
	"context"

	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type CommandGetter interface {
	GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error)
}

type baseCmd struct {
	CommandGetter
	log logrus.FieldLogger
}
