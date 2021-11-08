package cluster

import (
	context "context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
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

func (r *registrar) RegisterCluster(ctx context.Context, cluster *common.Cluster, v1Flag common.InfraEnvCreateFlag, v1ISOType models.ImageType) error {
	return r.registerCluster(ctx, cluster, models.ClusterStatusInsufficient, StatusInfoInsufficient, time.Now(), v1Flag, v1ISOType)
}

func (r *registrar) RegisterAddHostsCluster(ctx context.Context, cluster *common.Cluster, v1Flag common.InfraEnvCreateFlag, v1ISOType models.ImageType) error {
	return r.registerCluster(ctx, cluster, models.ClusterStatusAddingHosts, statusInfoAddingHosts, time.Now(), v1Flag, v1ISOType)
}

func (r *registrar) registerCluster(ctx context.Context, cluster *common.Cluster, status, statusInfo string, registerTime time.Time,
	v1Flag common.InfraEnvCreateFlag, v1ISOType models.ImageType) error {
	cluster.Status = swag.String(status)
	cluster.StatusInfo = swag.String(statusInfo)
	cluster.StatusUpdatedAt = strfmt.DateTime(registerTime)
	tx := r.db.Begin()
	success := false
	defer func() {
		if rec := recover(); rec != nil || !success {
			r.log.Error("update cluster failed")
			tx.Rollback()
		}
	}()
	if tx.Error != nil {
		r.log.WithError(tx.Error).Error("failed to start transaction")
	}

	queryParams := []string{"id = ?", cluster.ID.String()}
	if err := tx.First(&cluster, queryParams).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.log.WithError(err).Errorf("Error registering cluster %s", cluster.Name)
		return err
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		// Delete any previews record of the cluster if it was soft deleted in the past,
		// no error will be returned it wasn't existed.
		if err := tx.Unscoped().Delete(&cluster, queryParams).Error; err != nil {
			r.log.WithError(err).Errorf("Error registering cluster %s", cluster.Name)
			return errors.Wrapf(
				err,
				"error while trying to delete previews record from db (if exists) of cluster %s",
				cluster.ID.String())
		}
	}

	for _, tableName := range common.ClusterSubTables {
		tx = common.LoadTableFromDB(tx, tableName)
	}

	if err := tx.Create(cluster).Error; err != nil {
		r.log.Errorf("Error registering cluster %s", cluster.Name)
		return err
	}

	if v1Flag {
		err := common.CreateInfraEnvForCluster(tx, cluster, v1ISOType)
		if err != nil {
			r.log.WithError(err).Errorf("Failed to create Infra Env along the cluster %s", cluster.ID)
			return err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	success = true
	return nil
}

func (r *registrar) RegisterAddHostsOCPCluster(c *common.Cluster, db *gorm.DB) error {
	c.Status = swag.String(models.ClusterStatusAddingHosts)
	c.StatusInfo = swag.String(StatusInfoReady)
	err := db.Create(c).Error
	if err != nil {
		r.log.WithError(err).Errorf("Failed to create OCP cluster in DB")
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

	if txErr = common.DeleteRecordsByClusterID(tx, *cluster.ID, []interface{}{
		&models.MonitoredOperator{},
		&models.ClusterNetwork{},
		&models.ServiceNetwork{},
		&models.MachineNetwork{},
	}); txErr != nil {
		tx.Rollback()
		return errors.Errorf("failed to delete cluster records %s", cluster.ID)
	}

	if txErr = tx.Delete(cluster).Error; txErr != nil {
		tx.Rollback()
		return errors.Errorf("failed to delete cluster %s", cluster.ID)
	}

	infraEnv, err := common.GetInfraEnvFromDB(tx, *cluster.ID)
	if err == nil {
		if txErr = tx.Delete(infraEnv).Error; txErr != nil {
			tx.Rollback()
			return errors.Errorf("failed to delete infra-env %s", cluster.ID)
		}
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return errors.Errorf("failed to delete cluster %s, commit tx", cluster.ID)
	}
	return nil
}
