package cluster

import (
	context "context"

	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
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

func (r *registrar) RegisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	c.Status = swag.String(clusterStatusInsufficient)
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

	if err := tx.Preload("Hosts").Create(c).Error; err != nil {
		r.log.Errorf("Error registering cluster %s", c.Name)
		tx.Rollback()
		return &UpdateReply{
			State:     clusterStatusInsufficient,
			IsChanged: false,
		}, err
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		r.log.WithError(err).Errorf("failed to commit cluster %s changes on installation", c.ID.String())
		return &UpdateReply{
			State:     clusterStatusInsufficient,
			IsChanged: false,
		}, err
	}

	return &UpdateReply{
		State:     clusterStatusInsufficient,
		IsChanged: true,
	}, nil
}
