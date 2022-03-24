package migrations

import (
	"log"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

func multipleNetworksCleanup() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		var clusters []*models.Cluster
		var err error

		err = tx.Migrator().DropColumn(&clusters, "cluster_network_cidr")
		if err != nil {
			log.Printf("Non-critical error: %s", err)
		}
		err = tx.Migrator().DropColumn(&clusters, "cluster_network_host_prefix")
		if err != nil {
			log.Printf("Non-critical error: %s", err)
		}
		err = tx.Migrator().DropColumn(&clusters, "machine_network_cidr")
		if err != nil {
			log.Printf("Non-critical error: %s", err)
		}
		err = tx.Migrator().DropColumn(&clusters, "service_network_cidr")
		if err != nil {
			log.Printf("Non-critical error: %s", err)
		}

		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// As the migration never returns any errors, we are not implementing rollback.
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20220324203547",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
