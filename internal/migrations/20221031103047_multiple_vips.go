package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

func multipleVips() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		// WIP

		//dbClusters, err := common.GetClustersFromDBWhere(tx, common.UseEagerLoading, common.IncludeDeletedRecords)
		//if err != nil {
		//	return err
		//}
		//for _, cluster := range dbClusters {
		//	if cluster.APIVip != "" {
		//		apiVIPs := &models.APIVip{
		//			ClusterID: *cluster.ID,
		//			IP:        models.IP(cluster.APIVip),
		//		}
		//		if err = tx.Save(apiVIPs).Error; err != nil {
		//			return err
		//		}
		//	}
		//	if cluster.IngressVip != "" {
		//		ingressVIPs := &models.IngressVip{
		//			ClusterID: *cluster.ID,
		//			IP:        models.IP(cluster.IngressVip),
		//		}
		//		if err = tx.Save(ingressVIPs).Error; err != nil {
		//			return err
		//		}
		//	}
		//}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		for _, model := range []interface{}{
			&models.APIVip{},
			&models.IngressVip{},
		} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return err
			}
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20221031103047",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
