package infraenv

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

//go:generate mockgen --build_flags=--mod=mod -package=infraenv -destination=mock_infraenv_api.generated_go . API
type API interface {
	DeleteOrphanInfraEnvs(ctx context.Context, maxDeletePerInterval int, inactiveSince strfmt.DateTime) error
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

func (m Manager) DeleteOrphanInfraEnvs(ctx context.Context, maxDeletePerInterval int, inactiveSince strfmt.DateTime) error {
	log := logutil.FromContext(ctx, m.log)
	var infraEnvs []*models.InfraEnv
	if err := m.db.Limit(maxDeletePerInterval).Where("updated_at < ?", inactiveSince).Find(&infraEnvs).Error; err != nil {
		return err
	}
	for _, infraEnv := range infraEnvs {
		if infraEnv.ClusterID != "" {
			// Dereigster InfraEnv only if Cluster has been deleted
			_, err := common.GetClusterFromDBWhere(m.db, common.SkipEagerLoading,
				common.SkipDeletedRecords, "id = ?", infraEnv.ClusterID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = m.DeregisterInfraEnvAndHosts(ctx, *infraEnv.ID)
				if err != nil {
					log.WithError(err).Errorf("failed to deregister infraEnv %s", *infraEnv.ID)
					return err
				}
			} else if err != nil {
				log.WithError(err).Errorf("failed to GetClusterFromDBWhere infraEnv %s", *infraEnv.ID)
				return err
			}
		} else {
			err := m.DeregisterInfraEnvAndHosts(ctx, *infraEnv.ID)
			if err != nil {
				log.WithError(err).Errorf("failed to deregister infraEnv %s", *infraEnv.ID)
				return err
			}
		}
	}
	return nil
}

func (m Manager) DeregisterInfraEnvAndHosts(ctx context.Context, infraEnvId strfmt.UUID) error {
	log := logutil.FromContext(ctx, m.log)
	var hosts []*models.Host
	if reply := m.db.Where("infra_env_id = ?", infraEnvId).Delete(&hosts); reply.Error != nil {
		return reply.Error
	} else if reply.RowsAffected > 0 {
		m.log.Debugf("Deleted %s hosts from db", reply.RowsAffected)
	}
	err := m.DeregisterInfraEnv(ctx, infraEnvId)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", infraEnvId)
		return err
	}
	return nil
}

func (m Manager) DeregisterInfraEnv(ctx context.Context, infraEnvId strfmt.UUID) error {
	log := logutil.FromContext(ctx, m.log)
	infraEnv, err := common.GetInfraEnvFromDB(m.db, infraEnvId)
	if err != nil {
		return err
	}

	if err = m.db.Delete(infraEnv).Error; err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", infraEnvId)
		return err
	}
	return nil
}
