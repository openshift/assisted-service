package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func convertPrimaryIPStackColumnType() *gormigrate.Migration {

	migrate := func(tx *gorm.DB) error {
		// Check if the column is text/varchar (needs conversion)
		// Only convert if column exists AND is a text type
		var dataType string
		err := tx.Raw(`
			SELECT data_type FROM information_schema.columns 
			WHERE table_schema = current_schema() 
			  AND table_name = 'clusters' 
			  AND column_name = 'primary_ip_stack'
		`).Scan(&dataType).Error
		if err != nil {
			return err
		}

		// Only convert if column is a text/varchar type - skip for any other type
		// (including integer, bigint, or if column doesn't exist/not found)
		// PostgreSQL returns "text" for TEXT and "character varying" for VARCHAR
		if dataType != "text" && dataType != "character varying" {
			return nil
		}

		// Convert text values ('ipv4' -> 4, 'ipv6' -> 6) to integer
		// NULL values remain NULL
		return tx.Exec(`
			ALTER TABLE clusters 
			ALTER COLUMN primary_ip_stack TYPE integer 
			USING CASE 
				WHEN primary_ip_stack = 'ipv4' THEN 4 
				WHEN primary_ip_stack = 'ipv6' THEN 6 
				WHEN primary_ip_stack = '4' THEN 4
				WHEN primary_ip_stack = '6' THEN 6
				ELSE NULL 
			END
		`).Error
	}

	rollback := func(tx *gorm.DB) error {
		// Check if the column is integer/bigint (needs conversion to text)
		// Only convert if column exists AND is an integer type
		var dataType string
		err := tx.Raw(`
			SELECT data_type FROM information_schema.columns 
			WHERE table_schema = current_schema() 
			  AND table_name = 'clusters' 
			  AND column_name = 'primary_ip_stack'
		`).Scan(&dataType).Error
		if err != nil {
			return err
		}

		// Only convert if column is integer/bigint - skip for any other type
		// (including text, character varying, or if column doesn't exist)
		if dataType != "integer" && dataType != "bigint" {
			return nil
		}

		// Convert integer values (4 -> 'ipv4', 6 -> 'ipv6') back to text
		return tx.Exec(`
			ALTER TABLE clusters 
			ALTER COLUMN primary_ip_stack TYPE text
			USING CASE 
				WHEN primary_ip_stack = 4 THEN 'ipv4' 
				WHEN primary_ip_stack = 6 THEN 'ipv6' 
				ELSE NULL 
			END
		`).Error
	}

	return &gormigrate.Migration{
		ID:       "20251216120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
