package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func changeImageSSHKeyToText() *gormigrate.Migration {
	return migrateToText("20201202140700", "clusters", "image_ssh_public_key")
}
