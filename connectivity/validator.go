package connectivity

import (
	"encoding/json"
	"time"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validator.go -package=connectivity -destination=mock_connectivity_validator.go
type Validator interface {
	GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error)
}

func NewValidator(log logrus.FieldLogger) Validator {
	return &validator{
		log:   log,
		cache: common.NewExpiringCache(10*time.Minute, 10*time.Minute),
	}
}

type validator struct {
	log   logrus.FieldLogger
	cache common.ExpiringCache
}

func (v *validator) GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error) {
	key := common.GetHostKey(host)
	val, exists := v.cache.Get(key)
	if exists {
		value, ok := val.([]*models.Interface)
		if !ok {
			return nil, errors.Errorf("unexpected cast error for host %s infra-env %s", host.ID.String(), host.InfraEnvID.String())
		}
		return value, nil
	}
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	if len(inventory.Interfaces) == 0 {
		return nil, errors.Errorf("host %s doesn't have interfaces", host.ID)
	}
	v.cache.Set(key, inventory.Interfaces)
	return inventory.Interfaces, nil
}
