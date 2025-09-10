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

		db.Exec(`create index concurrently if not exists idx_hosts_infra_env_id on hosts (infra_env_id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_cluster_networks_cluster_id on cluster_networks (cluster_id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_service_networks_cluster_id on service_networks (cluster_id)`)
		if db.Error != nil {
			return db.Error
		}

		db.Exec(`create index concurrently if not exists idx_machine_networks_cluster_id on machine_networks (cluster_id)`)
		if db.Error != nil {
			return db.Error
		}

		return nil
	}

	rollback := func(db *gorm.DB) error {
		db.Exec(`drop index concurrently if exists idx_clusters_trigger_monitor_timestamp`)
		db.Exec(`drop index concurrently if exists idx_hosts_trigger_monitor_timestamp`)
		db.Exec(`drop index concurrently if exists idx_hosts_infra_env_id`)
		db.Exec(`drop index concurrently if exists idx_cluster_networks_cluster_id`)
		db.Exec(`drop index concurrently if exists idx_service_networks_cluster_id`)
		db.Exec(`drop index concurrently if exists idx_machine_networks_cluster_id`)
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20250115120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
