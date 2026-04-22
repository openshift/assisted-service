package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

var setEventsAutovacuumSettingsID = "20260413120000"

func setEventsAutovacuumSettings() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		// Set aggressive autovacuum thresholds for the high-churn events table
		if err := tx.Exec(`ALTER TABLE events SET (autovacuum_vacuum_scale_factor = 0.05)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`ALTER TABLE events SET (autovacuum_analyze_scale_factor = 0.025)`).Error; err != nil {
			return err
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// Reset to PostgreSQL defaults
		if err := tx.Exec(`ALTER TABLE events RESET (autovacuum_vacuum_scale_factor)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`ALTER TABLE events RESET (autovacuum_analyze_scale_factor)`).Error; err != nil {
			return err
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       setEventsAutovacuumSettingsID,
		Migrate:  migrate,
		Rollback: rollback,
	}
}
