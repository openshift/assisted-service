package connectivity

import (
	"encoding/json"
	"time"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validator.go -package=connectivity -destination=mock_connectivity_validator.generated_go
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
	key, err := common.GetInventoryInterfaces(host.Inventory)
	if err != nil {
		return nil, err
	}

	val, exists := v.cache.Get(key)
	if exists {
		value, ok := val.([]*models.Interface)
		if !ok {
			return nil, errors.Errorf("unexpected cast error for host %s infra-env %s", host.ID.String(), host.InfraEnvID.String())
		}
		return value, nil
	}

	var interfaces []*models.Interface
	if err := json.Unmarshal([]byte(key), &interfaces); err != nil {
		return nil, err
	}
	if len(interfaces) == 0 {
		return nil, errors.Errorf("host %s doesn't have interfaces", host.ID)
	}

	v.cache.Set(key, interfaces)
	return interfaces, nil
}
