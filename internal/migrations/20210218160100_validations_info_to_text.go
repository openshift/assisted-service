package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeClusterValidationsInfoToText() *gormigrate.Migration {
	return migrateToText("20210218160100", "clusters", "validations_info")
}
