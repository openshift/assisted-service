package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeOverridesToText() *gormigrate.Migration {
	return migrateToText("20201019194303", "clusters", "install_config_overrides")
}
