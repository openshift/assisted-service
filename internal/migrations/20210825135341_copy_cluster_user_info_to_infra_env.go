package migrations

import (
	"database/sql"
	"strings"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
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
				infraEnvUserName    sql.NullString
				infraEnvEmailDomain sql.NullString
				infraEnvOrgID       sql.NullString
				clusterUserName     sql.NullString
				clusterEmailDomain  sql.NullString
				clusterOrgID        sql.NullString
			)

			if err = rows.Scan(&infraEnvID, &infraEnvUserName, &infraEnvEmailDomain, &infraEnvOrgID, &clusterUserName, &clusterEmailDomain, &clusterOrgID); err != nil {
				return err
			}
			if err = rows.Err(); err != nil {
				return err
			}

			updates := make(map[string]interface{})
			var changed bool

			if infraEnvUserName.String == "" && clusterUserName.Valid {
				updates["user_name"] = clusterUserName.String
				changed = true
			}

			if infraEnvEmailDomain.String == "" && clusterEmailDomain.Valid {
				updates["email_domain"] = clusterEmailDomain.String
				changed = true
			}

			if infraEnvOrgID.String == "" && clusterOrgID.Valid {
				updates["org_id"] = clusterOrgID.String
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
