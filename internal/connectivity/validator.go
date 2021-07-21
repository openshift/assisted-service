package connectivity

import (
	"encoding/json"

	models "github.com/openshift/assisted-service/models/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validator.go -package=connectivity -destination=mock_connectivity_validator.go
type Validator interface {
	GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error)
}

func NewValidator(log logrus.FieldLogger) Validator {
	return &validator{
		log: log,
	}
}

type validator struct {
	log logrus.FieldLogger
}

func (v *validator) GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	if len(inventory.Interfaces) == 0 {
		return nil, errors.Errorf("host %s doesn't have interfaces", host.ID)
	}
	return inventory.Interfaces, nil
}
