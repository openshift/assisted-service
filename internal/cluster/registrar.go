package cluster

import (
	context "context"
	"time"

	"github.com/pkg/errors"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func NewRegistrar(log logrus.FieldLogger, db *gorm.DB) *registrar {
	return &registrar{
		log: log,
		db:  db,
	}
}

type registrar struct {
	log logrus.FieldLogger
	db  *gorm.DB
}

func (r *registrar) RegisterCluster(ctx context.Context, cluster *common.Cluster) error {
	return r.registerCluster(ctx, cluster, models.ClusterStatusInsufficient, statusInfoInsufficient, time.Now())
}

func (r *registrar) RegisterDay2Cluster(ctx context.Context, cluster *common.Cluster) error {
	return r.registerCluster(ctx, cluster, models.ClusterStatusDay2cluster, statusInfoDay2cluster, time.Now())
}

func (r *registrar) registerCluster(ctx context.Context, cluster *common.Cluster, status, statusInfo string, registerTime time.Time) error {
	cluster.Status = swag.String(status)
	cluster.StatusInfo = swag.String(statusInfo)
	cluster.StatusUpdatedAt = strfmt.DateTime(registerTime)
	tx := r.db.Begin()
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("update cluster failed")
			tx.Rollback()
		}
	}()
	if tx.Error != nil {
		r.log.WithError(tx.Error).Error("failed to start transaction")
	}

	if err := tx.Preload("Hosts").Create(cluster).Error; err != nil {
		r.log.Errorf("Error registering cluster %s", cluster.Name)
		tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		return err
	}

	return nil
}

func (r *registrar) DeregisterCluster(ctx context.Context, cluster *common.Cluster) error {
	var txErr error
	tx := r.db.Begin()

	defer func() {
		if txErr != nil {
			tx.Rollback()
		}
	}()

	if swag.StringValue(cluster.Status) == models.ClusterStatusInstalling {
		tx.Rollback()
		return errors.Errorf("cluster %s can not be removed while being installed", cluster.ID)
	}

	if txErr = tx.Where("cluster_id = ?", cluster.ID).Delete(&models.Host{}).Error; txErr != nil {
		tx.Rollback()
		return errors.Errorf("failed to deregister host while unregistering cluster %s", cluster.ID)
	}

	if txErr = tx.Delete(cluster).Error; txErr != nil {
		tx.Rollback()
		return errors.Errorf("failed to delete cluster %s", cluster.ID)
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return errors.Errorf("failed to delete cluster %s, commit tx", cluster.ID)
	}
	return nil
}
