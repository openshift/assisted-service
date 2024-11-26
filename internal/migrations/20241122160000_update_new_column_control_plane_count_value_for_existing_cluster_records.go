package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// split to 2 migrations, one for each query.

func updateNewColumnControlPlaneCountValueForExistingClusterRecords() *gormigrate.Migration {
	migrate := func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			err := tx.Model(&common.Cluster{}).
				// control_plane_count value in existing records can be NULL or 0 (default). We want to set the value in both cases
				Where(tx.Where("control_plane_count IS NULL OR control_plane_count = ?", 0)).
				Where("high_availability_mode = ?", models.ClusterCreateParamsHighAvailabilityModeNone).
				Update("control_plane_count", "1").Error
			if err != nil {
				return errors.Wrap(err, "failed to update control_plane_count value of existing SNO clusters to 1")
			}

			err = tx.Model(&common.Cluster{}).
				// control_plane_count value in existing records can be NULL or 0 (default). We want to set the value in both cases
				Where(tx.Where("control_plane_count IS NULL OR control_plane_count = ?", 0)).
				Where("high_availability_mode = ?", models.ClusterCreateParamsHighAvailabilityModeFull).
				Update("control_plane_count", "3").Error
			if err != nil {
				return errors.Wrap(err, "failed to update control_plane_count value of existing multi-node clusters to 3")
			}

			return nil
		})
	}

	rollback := func(tx *gorm.DB) error {
		// No rollback as we can't roll back only the modified records
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20241122160000",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
