package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

const deleteAgentUpgradeEventsID = "202212011120400"

func deleteAgentUpgradeEvents() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		// temporary migration - will need to switch this code to empty migration
		// in order to avoid missing migrations in the DB
		return tx.Where("name = ? or name = ?", "upgrade_agent_started", "upgrade_agent_finished").
			Delete(&common.Event{}).Error
	}

	rollback := func(tx *gorm.DB) error {
		return nil
	}

	return &gormigrate.Migration{
		ID:       deleteAgentUpgradeEventsID,
		Migrate:  migrate,
		Rollback: rollback,
	}
}
