package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeHostValidationsInfoToText() *gormigrate.Migration {
	return migrateToText("20210223090000", "hosts", "validations_info")
}
