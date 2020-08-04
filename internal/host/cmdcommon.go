package host

import (
	"context"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type baseCmd struct {
	log logrus.FieldLogger
}

type CommandGetter interface {
	GetStep(ctx context.Context, host *models.Host) (*models.Step, error)
}
