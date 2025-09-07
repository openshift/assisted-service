package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func addPerformanceIndexes() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		db.Exec(`create index concurrently if not exists idx_clusters_trigger_monitor_timestamp on clusters (trigger_monitor_timestamp, id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_hosts_trigger_monitor_timestamp on hosts (trigger_monitor_timestamp, cluster_id, infra_env_id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_hosts_cluster_infra_env on hosts (cluster_id, infra_env_id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_events_cluster_time on events (cluster_id, event_time)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_events_host_time on events (host_id, event_time)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_events_infraenv_time on events (infra_env_id, event_time)`)
		if db.Error != nil {
			return db.Error
		}

		return nil
	}

	rollback := func(db *gorm.DB) error {
		db.Exec(`drop index concurrently if exists idx_clusters_trigger_monitor_timestamp`)
		db.Exec(`drop index concurrently if exists idx_hosts_trigger_monitor_timestamp`)
		db.Exec(`drop index concurrently if exists idx_hosts_cluster_infra_env`)
		db.Exec(`drop index concurrently if exists idx_events_cluster_time`)
		db.Exec(`drop index concurrently if exists idx_events_host_time`)
		db.Exec(`drop index concurrently if exists idx_events_infraenv_time`)
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20250115120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
