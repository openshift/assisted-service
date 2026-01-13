package migrations

import (
	"fmt"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func convertNetworkColumnsToNativeTypes() *gormigrate.Migration {

	// columnConversion defines a column to convert
	type columnConversion struct {
		table      string
		column     string
		targetType string // "inet" for IPs, "cidr" for subnets
		isCidr     bool   // true if this is a CIDR column (requires network address validation)
	}

	// Using "inet" for IP columns and "cidr" for subnet columns:
	// - "inet" accepts IPs with optional prefix (permissive)
	// - "cidr" is strict and requires host bits to be zero (e.g., rejects 192.168.1.5/24)
	// - For cidr columns, we normalize using network(column::inet) to ensure valid network addresses
	// - PostgreSQL's family() function works on both inet and cidr types
	columns := []columnConversion{
		{table: "api_vips", column: "ip", targetType: "inet", isCidr: false},
		{table: "ingress_vips", column: "ip", targetType: "inet", isCidr: false},
		{table: "cluster_networks", column: "cidr", targetType: "cidr", isCidr: true},
		{table: "service_networks", column: "cidr", targetType: "cidr", isCidr: true},
		{table: "machine_networks", column: "cidr", targetType: "cidr", isCidr: true},
	}

	// cleanAndValidateNetworkData deletes empty/null rows and validates remaining data
	cleanAndValidateNetworkData := func(tx *gorm.DB, table, column string, isCidr bool) error {
		// Delete rows with empty or NULL values (these are invalid/orphaned entries)
		err := tx.Exec(fmt.Sprintf(`
			DELETE FROM %s WHERE %s IS NULL OR %s = ''
		`, table, column, column)).Error
		if err != nil {
			msg := fmt.Sprintf("failed to delete empty/null rows in %s.%s\nPlease manually remove them from the database", table, column)
			log.WithError(err).Error(msg)
			return errors.Wrap(err, msg)
		}

		// Check for values that don't look like valid IPs or CIDRs
		// Valid patterns: IPv4, IPv6, IPv4/prefix, IPv6/prefix
		// This regex is permissive - it catches obvious garbage but lets PostgreSQL do final validation
		var invalidValues []string
		err = tx.Raw(fmt.Sprintf(`
			SELECT DISTINCT %s FROM %s 
			WHERE %s !~ '^[0-9a-fA-F.:]+(/[0-9]+)?$'
			LIMIT 10
		`, column, table, column)).Scan(&invalidValues).Error
		if err != nil {
			msg := fmt.Sprintf("failed to validate values of %s.%s when converting DB column to inet",
				table, column)
			log.WithError(err).Error(msg)
			return errors.Wrap(err, msg)
		}

		if len(invalidValues) > 0 {
			msg := fmt.Sprintf("found invalid network values in %s.%s: %v. Please manually update them to valid values in the database or remove these rows. Regex used: ^[0-9a-fA-F.:]+(/[0-9]+)?$",
				table, column, invalidValues)
			log.Error(msg)
			return errors.New(msg)
		}

		// For CIDR columns, verify they have a prefix (e.g., /24)
		if isCidr {
			var missingPrefix []string
			err = tx.Raw(fmt.Sprintf(`
				SELECT DISTINCT %s FROM %s 
				WHERE %s !~ '/[0-9]+$'
				LIMIT 10
			`, column, table, column)).Scan(&missingPrefix).Error
			if err != nil {
				msg := fmt.Sprintf("failed to check CIDR prefix in %s.%s when converting DB column to cidr",
					table, column)
				log.WithError(err).Error(msg)
				return errors.Wrap(err, msg)
			}

			if len(missingPrefix) > 0 {
				msg := fmt.Sprintf("found CIDR values without prefix in %s.%s: %v. CIDRs must include prefix like /24\nPlease manually update them to valid values in the database or remove these rows",
					table, column, missingPrefix)
				log.Error(msg)
				return errors.New(msg)
			}
		}

		// Try a test conversion to inet to catch values that fail parsing
		var badCount int64
		err = tx.Raw(fmt.Sprintf(`
			SELECT COUNT(*) FROM %s 
			WHERE NOT (
				%s::text ~ '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+(/[0-9]+)?$'
				OR %s::text ~ '^[0-9a-fA-F:]+(/[0-9]+)?$'
			)
		`, table, column, column)).Scan(&badCount).Error
		if err != nil {
			// If the query itself fails, the data format might be really bad
			msg := fmt.Sprintf("failed to validate format of %s.%s when converting DB column to inet",
				table, column)
			log.WithError(err).Error(msg)
			return errors.Wrap(err, msg)
		}

		if badCount > 0 {
			msg := fmt.Sprintf("found invalid network values in %s.%s: %d.\nPlease manually update them to valid values in the database or remove these rows",
				table, column, badCount)
			log.Error(msg)
			return errors.New(msg)
		}

		return nil
	}

	migrate := func(tx *gorm.DB) error {
		for _, col := range columns {
			// Check current column type
			var dataType string
			err := tx.Raw(`
				SELECT data_type FROM information_schema.columns 
				WHERE table_name = ? AND column_name = ?
			`, col.table, col.column).Scan(&dataType).Error
			if err != nil {
				return err
			}

			// Only convert if column is text/varchar (not already inet/cidr)
			if dataType != "text" && dataType != "character varying" {
				continue
			}

			// Clean up empty/null rows and validate remaining data before conversion
			if cleanAndValidateErr := cleanAndValidateNetworkData(tx, col.table, col.column, col.isCidr); cleanAndValidateErr != nil {
				return errors.Wrapf(cleanAndValidateErr, "data cleanup/validation failed for %s.%s", col.table, col.column)
			}

			// Convert to native type
			// For CIDR columns: use network(column::inet) to normalize host bits to zero
			// For IP columns: use direct cast to inet
			usingClause := fmt.Sprintf("%s::%s", col.column, col.targetType)
			if col.isCidr {
				// network() extracts the network address from an inet value, returning a cidr
				// This normalizes values like "192.168.1.5/24" to "192.168.1.0/24"
				usingClause = fmt.Sprintf("network(%s::inet)", col.column)
			}

			err = tx.Exec(fmt.Sprintf(`
				ALTER TABLE %s
				ALTER COLUMN %s TYPE %s
				USING %s
			`, col.table, col.column, col.targetType, usingClause)).Error
			if err != nil {
				return errors.Wrapf(err, "failed to convert %s.%s to %s", col.table, col.column, col.targetType)
			}
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		for _, col := range columns {
			// Check current column type
			var dataType string
			err := tx.Raw(`
				SELECT data_type FROM information_schema.columns 
				WHERE table_name = ? AND column_name = ?
			`, col.table, col.column).Scan(&dataType).Error
			if err != nil {
				return errors.Wrapf(err, "failed to get column type for %s.%s", col.table, col.column)
			}

			// Only convert back if column is inet/cidr
			if dataType != "inet" && dataType != "cidr" {
				continue
			}

			// Convert back to text
			err = tx.Exec(fmt.Sprintf(`
				ALTER TABLE %s
				ALTER COLUMN %s TYPE text
				USING %s::text
			`, col.table, col.column, col.column)).Error
			if err != nil {
				return errors.Wrapf(err, "failed to convert %s.%s back to text", col.table, col.column)
			}
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20251217120000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
