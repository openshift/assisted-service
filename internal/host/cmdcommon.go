package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/sirupsen/logrus"
)

type baseCmd struct {
	log logrus.FieldLogger
}

type CommandGetter interface {
	GetStep(ctx context.Context, host *models.Host) (*models.Step, error)
}
