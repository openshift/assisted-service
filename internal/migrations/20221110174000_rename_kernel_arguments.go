package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const renameKernelArgumentsID = "20221110174000"

type infraEnv struct{}

func (infraEnv) TableName() string {
	return "infra_envs"
}

func renameKernelArguments() *gormigrate.Migration {

	migrate := func(tx *gorm.DB) error {
		if tx.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments") {
			if tx.Migrator().HasColumn(&infraEnv{}, "kernel_arguments") {

				// This was probably created by auto migrate
				if err := tx.Migrator().DropColumn(&infraEnv{}, "kernel_arguments"); err != nil {
					return err
				}
			}
			return tx.Migrator().RenameColumn(&infraEnv{}, "discovery_kernel_arguments", "kernel_arguments")
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		if tx.Migrator().HasColumn(&infraEnv{}, "kernel_arguments") {
			return tx.Migrator().RenameColumn(&infraEnv{}, "kernel_arguments", "discovery_kernel_arguments")
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       renameKernelArgumentsID,
		Migrate:  migrate,
		Rollback: rollback,
	}
}
