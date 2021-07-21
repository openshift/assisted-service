package migrations

import (
	"github.com/jinzhu/gorm"
	models "github.com/openshift/assisted-service/models/v1"
	gormigrate "gopkg.in/gormigrate.v1"
)

func changeHostValidationsInfoToText() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Model(&models.Host{}).ModifyColumn("validations_info", "text").Error
	}

	rollback := func(tx *gorm.DB) error {
		return tx.Model(&models.Host{}).ModifyColumn("validations_info", "varchar(2048)").Error
	}

	return &gormigrate.Migration{
		ID:       "20210223090000",
		Migrate:  migrate,
		Rollback: rollback,
	}
}
