package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeClusterSshKeyToText() *gormigrate.Migration {
	return migrateToText("20220113152127", "clusters", "ssh_public_key")
}
