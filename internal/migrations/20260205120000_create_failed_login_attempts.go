package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// FailedLoginAttempt tracks authentication failures for brute force protection.
// This model is defined here for the migration and also in pkg/auth/failed_attempts.go
// for runtime use.
type FailedLoginAttempt struct {
	ID             uint      `gorm:"primaryKey"`
	Identifier     string    `gorm:"size:255;not null;uniqueIndex:idx_failed_attempts_unique"`
	IdentifierType string    `gorm:"size:20;not null;uniqueIndex:idx_failed_attempts_unique"`
	AttemptCount   int       `gorm:"default:1"`
	FirstAttempt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	LastAttempt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	LockedUntil    *time.Time
}

func (FailedLoginAttempt) TableName() string {
	return "failed_login_attempts"
}

func createFailedLoginAttempts() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		// Create the failed_login_attempts table
		if err := tx.AutoMigrate(&FailedLoginAttempt{}); err != nil {
			return err
		}

		// Create additional index on locked_until for efficient cleanup queries
		// Use PostgreSQL partial index when available, otherwise fall back to standard index
		dialectName := tx.Dialector.Name()
		var indexSQL string
		if dialectName == "postgres" || dialectName == "postgresql" {
			// PostgreSQL supports partial indexes for better efficiency
			indexSQL = `
				CREATE INDEX IF NOT EXISTS idx_failed_attempts_locked
				ON failed_login_attempts(locked_until)
				WHERE locked_until IS NOT NULL
			`
		} else {
			// SQLite and other databases use a standard index
			indexSQL = `
				CREATE INDEX IF NOT EXISTS idx_failed_attempts_locked
				ON failed_login_attempts(locked_until)
			`
		}

		if err := tx.Exec(indexSQL).Error; err != nil {
			return err
		}

		return nil
	}

	rollback := func(tx *gorm.DB) error {
		return tx.Migrator().DropTable(&FailedLoginAttempt{})
	}

	return &gormigrate.Migration{
		ID:       "20260205120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
