package migrations

import (
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/dbc"
	gormigrate "gopkg.in/gormigrate.v1"
)

func changeImageSSHKeyToText() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Model(&dbc.Cluster{}).ModifyColumn("image_ssh_public_key", "text").Error
	}

	rollback := func(tx *gorm.DB) error {
		return tx.Model(&dbc.Cluster{}).ModifyColumn("image_ssh_public_key", "varchar(1024)").Error
	}

	return &gormigrate.Migration{
		ID:       "20201202140700",
		Migrate:  migrate,
		Rollback: rollback,
	}
}
