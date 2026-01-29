package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func createRevokedTokensTable() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		// Create the revoked_tokens table for token blacklisting
		// Note: No deleted_at column - tokens use hard deletes based on expires_at
		err := db.Exec(`
			CREATE TABLE IF NOT EXISTS revoked_tokens (
				id SERIAL PRIMARY KEY,
				created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
				token_hash VARCHAR(64) NOT NULL,
				revoked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
				expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
				entity_id VARCHAR(255),
				entity_type VARCHAR(50),
				reason VARCHAR(255)
			)
		`).Error
		if err != nil {
			return err
		}

		// Create unique index on token_hash for fast lookups
		err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_revoked_tokens_hash ON revoked_tokens(token_hash)`).Error
		if err != nil {
			return err
		}

		// Create index on expires_at for cleanup job
		err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires ON revoked_tokens(expires_at)`).Error
		if err != nil {
			return err
		}

		// Create index on created_at
		err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_revoked_tokens_created_at ON revoked_tokens(created_at)`).Error
		if err != nil {
			return err
		}

		return nil
	}

	rollback := func(db *gorm.DB) error {
		return db.Exec(`DROP TABLE IF EXISTS revoked_tokens`).Error
	}

	return &gormigrate.Migration{
		ID:       "20260127120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
