package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func updateOciToExternalPlatformType() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		err := tx.Exec("UPDATE clusters SET platform_type=?, platform_external_platform_name=?, platform_external_cloud_controller_manager=? WHERE platform_type=?", "external", "oci", "External", "oci").Error
		return err
	}

	rollback := func(tx *gorm.DB) error {
		err := tx.Exec("UPDATE clusters SET platform_type=?, platform_external_platform_name=NULL, platform_external_cloud_controller_manager=NULL WHERE platform_type=? AND platform_external_platform_name=? AND platform_external_cloud_controller_manager=?", "oci", "external", "oci", "External").Error
		return err
	}

	return &gormigrate.Migration{
		ID:       "20231219105000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
