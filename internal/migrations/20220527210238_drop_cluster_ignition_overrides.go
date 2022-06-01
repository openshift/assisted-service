package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func dropClusterIgnitionOverrides() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Exec("ALTER TABLE clusters DROP COLUMN IF EXISTS ignition_config_overrides").Error
	}

	rollback := func(tx *gorm.DB) error {
		// No rollback because automigrate will add the column back for a source version with the column
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20220527210238",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
