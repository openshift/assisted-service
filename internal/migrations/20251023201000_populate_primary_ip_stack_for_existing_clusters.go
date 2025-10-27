package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
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
			var dualStackClusterIDs []strfmt.UUID

			err := tx.
				Preload("MachineNetworks").
				Preload("ServiceNetworks").
				Preload("ClusterNetworks").
				Where("primary_ip_stack IS NULL").
				FindInBatches(&clusters, batchSize, func(batchTx *gorm.DB, batch int) error {
					// Collect IDs of dual-stack clusters from this batch
					for _, cluster := range clusters {
						c := cluster

						// Skip if not dual-stack (PrimaryIPStack should remain nil for single-stack)
						if !network.CheckIfClusterIsDualStack(&c) {
							continue
						}

						dualStackClusterIDs = append(dualStackClusterIDs, *c.ID)
					}
					return nil
				}).Error

			if err != nil {
				return errors.Wrap(err, "failed to collect dual-stack cluster IDs")
			}

			// Single bulk update for all dual-stack clusters
			if len(dualStackClusterIDs) > 0 {
				err = tx.Model(&common.Cluster{}).
					Where("id IN ?", dualStackClusterIDs).
					Update("primary_ip_stack", common.PrimaryIPStackV4).Error
				if err != nil {
					return errors.Wrap(err, "failed to bulk update primary_ip_stack for dual-stack clusters")
				}
			}

			return nil
		})
	}

	rollback := func(tx *gorm.DB) error {
		// Set primary_ip_stack back to NULL for all clusters that had it set by this migration
		// (all clusters where primary_ip_stack = 'IPv4')
		err := tx.Model(&common.Cluster{}).
			Where("primary_ip_stack = ?", common.PrimaryIPStackV4).
			Update("primary_ip_stack", nil).Error
		if err != nil {
			return errors.Wrap(err, "failed to rollback primary_ip_stack")
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20251023201000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
