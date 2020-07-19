package common

import (
	"fmt"
	"strings"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/ory/dockertest/v3"

	"github.com/filanov/bm-inventory/models"
	. "github.com/onsi/gomega"
)

type DBContext struct {
	resource *dockertest.Resource
	pool     *dockertest.Pool
}

var gDbCtx DBContext = DBContext{
	resource: nil,
	pool:     nil,
}

func InitializeDBTest() {
	pool, err := dockertest.NewPool("")
	Expect(err).ShouldNot(HaveOccurred())

	resource, err := pool.Run("postgres", "12.3", []string{"POSTGRES_PASSWORD=admin", "POSTGRES_USER=admin"})
	Expect(err).ShouldNot(HaveOccurred())

	gDbCtx.pool = pool
	gDbCtx.resource = resource
}

func TerminateDBTest() {
	Expect(gDbCtx).ShouldNot(BeNil())
	err := gDbCtx.pool.Purge(gDbCtx.resource)
	Expect(err).ShouldNot(HaveOccurred())
	gDbCtx.pool = nil
}

func PrepareTestDB(dbName string, extrasSchemas ...interface{}) *gorm.DB {
	Expect(gDbCtx.pool).ShouldNot(BeNil())
	Expect(gDbCtx.resource).ShouldNot(BeNil())
	var dbTemp *gorm.DB
	err := gDbCtx.pool.Retry(func() error {
		var err error

		dbTemp, err = gorm.Open("postgres", fmt.Sprintf("host=localhost port=%s user=admin password=admin sslmode=disable", gDbCtx.resource.GetPort("5432/tcp")))
		return err
	})
	Expect(err).ShouldNot(HaveOccurred())
	defer dbTemp.Close()

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", strings.ToLower(dbName)))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=localhost port=%s dbname=%s user=admin password=admin sslmode=disable", gDbCtx.resource.GetPort("5432/tcp"), strings.ToLower(dbName)))
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

	Expect(gDbCtx.resource).ShouldNot(BeNil())
	db.Close()

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=localhost port=%s user=admin password=admin sslmode=disable", gDbCtx.resource.GetPort("5432/tcp")))
	Expect(err).ShouldNot(HaveOccurred())
	defer db.Close()
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", strings.ToLower(dbName)))

	Expect(db.Error).ShouldNot(HaveOccurred())
}
