package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeClusterSshKeyToText() *gormigrate.Migration {
	return migrateToText("20211108150559", "clusters", "ssh_public_key")
}
