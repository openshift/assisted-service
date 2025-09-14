package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func populatePrimaryIPStackForExistingClusters() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			// Find all clusters that don't have PrimaryIPStack set yet
			var clusters []common.Cluster
			err := tx.Preload("MachineNetworks").Preload("APIVips").Preload("IngressVips").Preload("ServiceNetworks").Preload("ClusterNetworks").Where("primary_ip_stack IS NULL").Find(&clusters).Error
			if err != nil {
				return errors.Wrap(err, "failed to fetch clusters without primary_ip_stack")
			}

			for _, cluster := range clusters {
				// Skip if not dual-stack (PrimaryIPStack should remain nil for single-stack)
				if !network.CheckIfClusterIsDualStack(&cluster) {
					continue
				}

				// Determine primary IP stack based on existing network configuration
				primaryStack, err := network.SetPrimaryIPStack(
					cluster.MachineNetworks,
					cluster.APIVips,
					cluster.IngressVips,
					cluster.ServiceNetworks,
					cluster.ClusterNetworks,
				)
				if err != nil {
					// Log error but continue with other clusters
					// In case of inconsistent configuration, leave as nil
					continue
				}

				// Update the cluster with the determined primary stack
				if primaryStack != nil {
					err = tx.Model(&cluster).Where("id = ?", cluster.ID).Update("primary_ip_stack", *primaryStack).Error
				} else {
					// Skip if primaryStack is nil (shouldn't happen for dual-stack, but safety check)
					continue
				}
				if err != nil {
					return errors.Wrapf(err, "failed to update primary_ip_stack for cluster %s", cluster.ID)
				}
			}

			return nil
		})
	}

	rollback := func(tx *gorm.DB) error { return nil }

	return &gormigrate.Migration{
		ID:       "20250918174600",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
