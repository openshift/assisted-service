package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func modifyEventsId() *gormigrate.Migration {
	return migrateColumn("2021115162400", "events", "id", "integer", "bigint")
}
