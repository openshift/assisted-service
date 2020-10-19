package migrations

import (
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/models"
	gormigrate "gopkg.in/gormigrate.v1"
)

func changeOverridesToText() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Model(&models.Cluster{}).ModifyColumn("install_config_overrides", "text").Error
	}

	rollback := func(tx *gorm.DB) error {
		return tx.Model(&models.Cluster{}).ModifyColumn("install_config_overrides", "varchar(2048)").Error
	}

	return &gormigrate.Migration{
		ID:       "20201019194303",
		Migrate:  migrate,
		Rollback: rollback,
	}
}
