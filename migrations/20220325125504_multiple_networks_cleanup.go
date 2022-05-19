package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

func multipleNetworksCleanup() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&common.Cluster{}) {
			// Clear values of the selected columns. We don't need to handle errors as those
			// columns are meant to disappear in the future, so this migration should gracefuly
			// handle the scenario when those do not exist.
			tx.Exec("UPDATE clusters SET cluster_network_cidr='';")
			tx.Exec("UPDATE clusters SET cluster_network_host_prefix=0;")
			tx.Exec("UPDATE clusters SET machine_network_cidr='';")
			tx.Exec("UPDATE clusters SET service_network_cidr='';")
		}

		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// Don't really want to ever recalculate values back.
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20220325125504",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
