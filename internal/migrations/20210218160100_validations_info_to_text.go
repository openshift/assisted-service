package migrations

import (
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	gormigrate "gopkg.in/gormigrate.v1"
)

func changeClusterValidationsInfoToText() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Model(&common.Cluster{}).ModifyColumn("validations_info", "text").Error
	}

	rollback := func(tx *gorm.DB) error {
		return tx.Model(&common.Cluster{}).ModifyColumn("validations_info", "varchar(2048)").Error
	}

	return &gormigrate.Migration{
		ID:       "20210218160100",
		Migrate:  migrate,
		Rollback: rollback,
	}
}
