package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

func multipleVips() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		for _, model := range []interface{}{
			&models.APIVip{},
			&models.IngressVip{},
		} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return err
			}
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20221031103047",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
