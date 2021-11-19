package migrations

import (
	"crypto/rand"
	"encoding/hex"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

func createInfraEnvImageTokenKey() *gormigrate.Migration {
	// Effectively a copy of gencrypto.HMACKey
	createNewKey := func() (string, error) {
		buf := make([]byte, 32)
		_, err := rand.Read(buf)
		if err != nil {
			return "", err
		}

		return hex.EncodeToString(buf), nil
	}

	migrate := func(tx *gorm.DB) error {
		rows, err := tx.Model(&common.InfraEnv{}).Where("image_token_key is null or image_token_key = ''").Select("id").Rows()
		if err != nil {
			return err
		}

		for rows.Next() {
			var infraEnvID strfmt.UUID
			if err = rows.Scan(&infraEnvID); err != nil {
				return err
			}

			if err = rows.Err(); err != nil {
				return err
			}

			key, err := createNewKey()
			if err != nil {
				return err
			}

			if err = tx.Model(&common.InfraEnv{}).Where("id = ?", infraEnvID).Update("image_token_key", key).Error; err != nil {
				return err
			}
		}

		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// This data shouldn't be removed so rollback will be a no-op
		return nil
	}

	return &gormigrate.Migration{
		ID:       "20211108163809",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
