package migrations

import (
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	gormigrate "gopkg.in/gormigrate.v1"
)

func populateInfraEnv() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {

		if tx.HasTable(&common.Host{}) {
			// Generate the infra_env_id column
			if err := tx.Exec("ALTER TABLE hosts ADD COLUMN IF NOT EXISTS infra_env_id text NULL;").Error; err != nil {
				return err
			}
			// Populate the infra_env_id column
			if err := tx.Exec("UPDATE hosts SET infra_env_id=cluster_id;").Error; err != nil {
				return err
			}
		}

		if tx.HasTable(&common.Cluster{}) {
			// Generate the InfraEnv table
			if err := tx.AutoMigrate(&common.InfraEnv{}).Error; err != nil {
				return err
			}

			if err := tx.AutoMigrate(&common.Cluster{}).Error; err != nil {
				return err
			}

			dbClusters, err := common.GetClustersFromDBWhere(tx, common.UseEagerLoading, common.IncludeDeletedRecords)
			if err != nil {
				return err
			}
			for _, cluster := range dbClusters {
				infraEnvID := *cluster.ID
				infraenv := common.InfraEnv{
					PullSecret:     cluster.PullSecret,
					ImageExpiresAt: cluster.ImageInfo.ExpiresAt,
					GeneratedAt:    cluster.ImageInfo.CreatedAt,
					Generated:      cluster.ImageGenerated,
					ProxyHash:      cluster.ProxyHash,
					InfraEnv: models.InfraEnv{
						ID:                     infraEnvID,
						AdditionalNtpSources:   cluster.AdditionalNtpSource,
						ClusterID:              *cluster.ID,
						Href:                   *cluster.Href,
						IgnitionConfigOverride: cluster.IgnitionConfigOverrides,
						Kind:                   models.InfraEnvKindInfraEnv,
						Name:                   cluster.Name,
						Proxy: &models.Proxy{
							HTTPProxy:  &cluster.HTTPProxy,
							HTTPSProxy: &cluster.HTTPSProxy,
							NoProxy:    &cluster.NoProxy,
						},
						PullSecretSet:       cluster.PullSecretSet,
						DownloadURL:         cluster.ImageInfo.DownloadURL,
						GeneratorVersion:    cluster.ImageInfo.GeneratorVersion,
						SizeBytes:           cluster.ImageInfo.SizeBytes,
						SSHAuthorizedKey:    cluster.ImageInfo.SSHPublicKey,
						StaticNetworkConfig: cluster.ImageInfo.StaticNetworkConfig,
						Type:                cluster.ImageInfo.Type,
						OpenshiftVersion:    cluster.OpenshiftVersion,
					},
				}
				tx.Create(&infraenv)

				cluster_updates := map[string]interface{}{}
				cluster_updates["static_network_configured"] = (cluster.ImageInfo.StaticNetworkConfig != "")
				tx.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(cluster_updates)
			}
		}

		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// TODO
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20210713123129",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
