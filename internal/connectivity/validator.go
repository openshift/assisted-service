package connectivity

import (
	"encoding/json"
	"fmt"

	"github.com/filanov/bm-inventory/models"
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
		return nil, fmt.Errorf("host %s doesn't have interfaces", host.ID)
	}
	return inventory.Interfaces, nil
}
