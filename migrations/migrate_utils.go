package migrations

import (
	"fmt"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func migrateColumn(id, table, columnName, srcType, dstType string) *gormigrate.Migration {
	run := func(tx *gorm.DB, columnType string) error {
		if tx.Migrator().HasColumn(table, columnName) {
			stmt := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", table, columnName, columnType)
			return tx.Exec(stmt).Error
		}
		return nil
	}

	migrate := func(tx *gorm.DB) error {
		return run(tx, dstType)
	}

	rollback := func(tx *gorm.DB) error {
		return run(tx, srcType)
	}

	return &gormigrate.Migration{
		ID:       id,
		Migrate:  migrate,
		Rollback: rollback,
	}
}

func migrateToText(id, table, columnName string) *gormigrate.Migration {
	return migrateColumn(id, table, columnName, "varchar(2048)", "text")
}
