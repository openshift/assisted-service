package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func addPerformanceIndexes() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		db.Exec(`create index concurrently if not exists idx_hosts_infra_env_id on hosts (infra_env_id)`)
		if db.Error != nil {
			return db.Error
		}

		return nil
	}

	rollback := func(db *gorm.DB) error {
		db.Exec(`drop index concurrently if exists idx_hosts_infra_env_id`)
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20250115120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
