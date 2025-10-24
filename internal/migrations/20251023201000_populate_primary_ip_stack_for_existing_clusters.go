package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func populatePrimaryIPStackForExistingClusters() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			const batchSize = 10 // Process 10 clusters at a time to avoid memory issues
			var clusters []common.Cluster

			err := tx.
				Preload("MachineNetworks").
				Preload("ServiceNetworks").
				Preload("ClusterNetworks").
				Where("primary_ip_stack IS NULL").
				FindInBatches(&clusters, batchSize, func(batchTx *gorm.DB, batch int) error {
					for _, cluster := range clusters {
						c := cluster

						// Skip if not dual-stack (PrimaryIPStack should remain nil for single-stack)
						if !network.CheckIfClusterIsDualStack(&c) {
							continue
						}

						// For dual-stack clusters, set primary IP stack to IPv4 (consistent default)
						err := tx.Model(&c).Where("id = ?", c.ID).Update("primary_ip_stack", common.PrimaryIPStackV4).Error
						if err != nil {
							return errors.Wrapf(err, "failed to update primary_ip_stack for cluster %s", c.ID)
						}
					}
					return nil
				}).Error

			return err
		})
	}

	rollback := func(tx *gorm.DB) error { return nil }

	return &gormigrate.Migration{
		ID:       "20251023201000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
