package infraenv

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

//go:generate mockgen -package=infraenv -destination=mock_infraenv_api.go . API
type API interface {
	DeregisterInfraEnv(ctx context.Context, infraEnvId strfmt.UUID) error
}

type Manager struct {
	log           logrus.FieldLogger
	db            *gorm.DB
	objectHandler s3wrapper.API
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, objectHandler s3wrapper.API) *Manager {
	return &Manager{
		log:           log,
		db:            db,
		objectHandler: objectHandler,
	}
}

func (m Manager) DeregisterInfraEnv(ctx context.Context, infraEnvId strfmt.UUID) error {
	log := logutil.FromContext(ctx, m.log)
	infraEnv, err := common.GetInfraEnvFromDB(m.db, infraEnvId)
	if err != nil {
		return err
	}
	// Delete discovery image for deregistered infraEnv
	discoveryImage := fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, infraEnvId.String()))
	exists, err := m.objectHandler.DoesObjectExist(ctx, discoveryImage)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", infraEnvId)
		return err
	}
	if exists {
		_, err = m.objectHandler.DeleteObject(ctx, discoveryImage)
		if err != nil {
			log.WithError(err).Errorf("failed to deregister infraEnv %s", infraEnvId)
			return err
		}
	}

	if err = m.db.Delete(infraEnv).Error; err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", infraEnvId)
		return err
	}
	return nil
}
