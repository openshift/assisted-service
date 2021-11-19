package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

func multipleNetworks() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		dbClusters, err := common.GetClustersFromDBWhere(tx, common.UseEagerLoading, common.IncludeDeletedRecords)
		if err != nil {
			return err
		}
		for _, cluster := range dbClusters {
			if cluster.ClusterNetworkCidr != "" {
				clusterNetwork := &models.ClusterNetwork{
					ClusterID:  *cluster.ID,
					Cidr:       models.Subnet(cluster.ClusterNetworkCidr),
					HostPrefix: cluster.ClusterNetworkHostPrefix,
				}
				if err = tx.Save(clusterNetwork).Error; err != nil {
					return err
				}
			}

			if cluster.ServiceNetworkCidr != "" {
				serviceNetwork := &models.ServiceNetwork{
					ClusterID: *cluster.ID,
					Cidr:      models.Subnet(cluster.ServiceNetworkCidr),
				}
				if err = tx.Save(serviceNetwork).Error; err != nil {
					return err
				}
			}

			if cluster.MachineNetworkCidr != "" {
				machineNetwork := &models.MachineNetwork{Cidr: models.Subnet(cluster.MachineNetworkCidr), ClusterID: *cluster.ID}
				if err = tx.Save(machineNetwork).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		for _, model := range []interface{}{
			&models.ClusterNetwork{},
			&models.ServiceNetwork{},
			&models.MachineNetwork{},
		} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return err
			}
		}
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20210822134659",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
