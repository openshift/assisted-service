package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func addHostsByClusterIdIndex() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		db.Exec(`create index if not exists hosts_by_cluster_id on hosts (cluster_id)`)
		return db.Error
	}

	rollback := func(db *gorm.DB) error {
		db.Exec(`drop index if exists hosts_by_cluster_id`)
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20250728120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
