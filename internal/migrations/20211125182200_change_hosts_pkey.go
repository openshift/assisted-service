package migrations

import (
	"fmt"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const MIGRATE_HOSTS_PKEY_ID = "21211125182200"

func getHostsPkeyColumns(db *gorm.DB) ([]string, error) {
	var columnNames []string
	err := db.Raw(`SELECT a.attname FROM pg_index i JOIN
				pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
				WHERE i.indrelid = 'hosts'::regclass AND i.indisprimary`).Pluck("attrname", &columnNames).Error
	return columnNames, err
}

func changeHostsPkey(db *gorm.DB, from, to string) error {
	columnNames, err := getHostsPkeyColumns(db)
	if err != nil {
		return err
	}
	if len(columnNames) != 2 || !funk.Every(columnNames, "id", to) {
		if err = db.Exec("ALTER TABLE hosts DROP CONSTRAINT hosts_pkey").Error; err != nil {
			return err
		}
		if err = db.Exec(fmt.Sprintf("ALTER TABLE hosts ALTER COLUMN %s SET NOT NULL", to)).Error; err != nil {
			return err
		}
		if err = db.Exec(fmt.Sprintf("ALTER TABLE hosts ALTER COLUMN %s DROP NOT NULL", from)).Error; err != nil {
			return err
		}
		err = db.Exec(fmt.Sprintf("ALTER TABLE hosts ADD PRIMARY KEY (id, %s)", to)).Error
	}
	return err
}

func upgradeHostsPkey(db *gorm.DB) error {
	return changeHostsPkey(db, "cluster_id", "infra_env_id")
}

func rollbackHostsPkey(db *gorm.DB) error {
	return changeHostsPkey(db, "infra_env_id", "cluster_id")
}

func migrateHostsPkey() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID:       MIGRATE_HOSTS_PKEY_ID,
		Migrate:  upgradeHostsPkey,
		Rollback: rollbackHostsPkey,
	}
}
