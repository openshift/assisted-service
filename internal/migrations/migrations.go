package migrations

import (
	"sort"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func MigratePre(db *gorm.DB) error {
	return gormigrate.New(db, gormigrate.DefaultOptions, pre()).Migrate()
}

func pre() []*gormigrate.Migration {
	preMigrations := []*gormigrate.Migration{
		modifyEventsId(),
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
		changeClusterSshKeyToText(),
		changeClusterValidationsInfoToText(),
		changeHostValidationsInfoToText(),
		multipleNetworks(),
		copyClusterUserInfoToInfraEnv(),
		createInfraEnvImageTokenKey(),
		migrateHostsPkey(),
		changeStaticConfigFormat(),
		multipleNetworksCleanup(),
		dropClusterIgnitionOverrides(),
		multipleVips(),
	}

	sort.SliceStable(postMigrations, func(i, j int) bool { return postMigrations[i].ID < postMigrations[j].ID })

	return postMigrations
}
