package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func dropClusterApiVipAndIngressVip() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		var err error

		if err = tx.Exec("ALTER TABLE clusters DROP COLUMN IF EXISTS api_vip").Error; err != nil {
			return err
		}
		if err = tx.Exec("ALTER TABLE clusters DROP COLUMN IF EXISTS ingress_vip").Error; err != nil {
			return err
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// No rollback because automigrate will add the column back for a source version with the column
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20231010101010",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
