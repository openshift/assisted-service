package subsystem

import "github.com/filanov/bm-inventory/models"

func clearDB() {
	db.Delete(&models.Image{})
	db.Delete(&models.Node{})
	db.Delete(&models.Cluster{})
}
