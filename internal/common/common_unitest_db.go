package common

import (
	"fmt"
	"strings"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"

	"github.com/filanov/bm-inventory/models"
	. "github.com/onsi/gomega"
)

func PrepareTestDB(dbName string, extrasSchemas ...interface{}) *gorm.DB {
	dbTemp, err := gorm.Open("postgres", "host=127.0.0.1 port=5432 user=admin password=admin sslmode=disable")
	Expect(err).ShouldNot(HaveOccurred())
	defer dbTemp.Close()

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", strings.ToLower(dbName)))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=5432 dbname=%s user=admin password=admin sslmode=disable", strings.ToLower(dbName)))
	Expect(err).ShouldNot(HaveOccurred())
	// db = db.Debug()
	db.AutoMigrate(&models.Host{}, &Cluster{})
	if len(extrasSchemas) > 0 {
		for _, schema := range extrasSchemas {
			db = db.AutoMigrate(schema)
			Expect(db.Error).ShouldNot(HaveOccurred())
		}
	}
	return db
}

func DeleteTestDB(db *gorm.DB, dbName string) {
	db.Close()
	db, err := gorm.Open("postgres", "host=127.0.0.1 port=5432 user=admin password=admin sslmode=disable")
	Expect(err).ShouldNot(HaveOccurred())
	defer db.Close()
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", strings.ToLower(dbName)))

	Expect(db.Error).ShouldNot(HaveOccurred())
}
