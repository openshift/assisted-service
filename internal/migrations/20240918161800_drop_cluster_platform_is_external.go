package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func dropClusterPlatformIsExternal() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		var err error

		if err = tx.Exec("ALTER TABLE clusters DROP COLUMN IF EXISTS platform_is_external").Error; err != nil {
			return err
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// No rollback because automigrate will add the column back for a source version with the column
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20240918161800",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
