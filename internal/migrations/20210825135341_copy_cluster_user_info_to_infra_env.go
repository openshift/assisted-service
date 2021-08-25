package migrations

import (
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	gormigrate "gopkg.in/gormigrate.v1"
)

func copyClusterUserInfoToInfraEnv() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		selectFields := []string{
			"infra_envs.id",
			"infra_envs.user_name",
			"infra_envs.email_domain",
			"infra_envs.org_id",
			"clusters.user_name",
			"clusters.email_domain",
			"clusters.org_id",
		}
		rows, err := tx.Model(&common.InfraEnv{}).Joins("JOIN clusters ON clusters.id = infra_envs.cluster_id").Select(strings.Join(selectFields, ",")).Rows()
		if err != nil {
			return err
		}

		for rows.Next() {
			var (
				infraEnvID          strfmt.UUID
				infraEnvUserName    string
				infraEnvEmailDomain string
				infraEnvOrgID       string
				clusterUserName     string
				clusterEmailDomain  string
				clusterOrgID        string
			)

			if err = rows.Scan(&infraEnvID, &infraEnvUserName, &infraEnvEmailDomain, &infraEnvOrgID, &clusterUserName, &clusterEmailDomain, &clusterOrgID); err != nil {
				return err
			}
			if err = rows.Err(); err != nil {
				return err
			}

			updates := make(map[string]interface{})
			var changed bool

			if infraEnvUserName == "" && clusterUserName != "" {
				updates["user_name"] = clusterUserName
				changed = true
			}

			if infraEnvEmailDomain == "" && clusterEmailDomain != "" {
				updates["email_domain"] = clusterEmailDomain
				changed = true
			}

			if infraEnvOrgID == "" && clusterOrgID != "" {
				updates["org_id"] = clusterOrgID
				changed = true
			}

			if changed {
				err = tx.Model(&common.InfraEnv{}).Where("id = ?", infraEnvID).Updates(updates).Error
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// Don't really want to ever remove the user info from the cluster
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20210825135341",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
