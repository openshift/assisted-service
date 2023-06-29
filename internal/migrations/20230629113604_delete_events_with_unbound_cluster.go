package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

func deleteEventsWithUnboundCluster() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		db := tx.Unscoped()
		return db.Where("cluster_id NOT IN (SELECT id FROM clusters)").Delete(&models.Event{}).Error
	}
	rollback := func(tx *gorm.DB) error {
		// Can't really rollback a delete
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20230629113604",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
