package migrations

import (
	"sort"

	"github.com/jinzhu/gorm"
	gormigrate "gopkg.in/gormigrate.v1"
)

func MigratePre(db *gorm.DB) error {
	return gormigrate.New(db, gormigrate.DefaultOptions, pre()).Migrate()
}

func pre() []*gormigrate.Migration {
	preMigrations := []*gormigrate.Migration{
		populateInfraEnv(),
	}

	sort.SliceStable(preMigrations, func(i, j int) bool { return preMigrations[i].ID < preMigrations[j].ID })

	return preMigrations
}

func MigratePost(db *gorm.DB) error {
	return gormigrate.New(db, gormigrate.DefaultOptions, post()).Migrate()
}

func post() []*gormigrate.Migration {
	postMigrations := []*gormigrate.Migration{
		changeOverridesToText(),
		changeImageSSHKeyToText(),
		changeClusterValidationsInfoToText(),
		changeHostValidationsInfoToText(),
	}

	sort.SliceStable(postMigrations, func(i, j int) bool { return postMigrations[i].ID < postMigrations[j].ID })

	return postMigrations
}
