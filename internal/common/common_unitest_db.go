package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/ory/dockertest/v3"

	"github.com/filanov/bm-inventory/models"
	. "github.com/onsi/gomega"
)

const (
	dbDockerName  = "ut-postgres"
	dbDefaultPort = "5432"
)

type DBContext struct {
	resource *dockertest.Resource
	pool     *dockertest.Pool
}

func (c *DBContext) GetPort() string {
	if c.resource == nil {
		return dbDefaultPort
	} else {
		return c.resource.GetPort(fmt.Sprintf("%s/tcp", dbDefaultPort))
	}
}

var gDbCtx DBContext = DBContext{
	resource: nil,
	pool:     nil,
}

func InitializeDBTest() {
	if os.Getenv("SKIP_UT_DB") != "" {
		return
	}
	pool, err := dockertest.NewPool("")
	Expect(err).ShouldNot(HaveOccurred())

	//cleanup any old instances of the DB
	if oldResource, isFound := pool.ContainerByName(dbDockerName); isFound {
		oldResource.Close()
	}
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "12.3",
		Env:        []string{"POSTGRES_PASSWORD=admin", "POSTGRES_USER=admin"},
		Name:       dbDockerName,
	})
	Expect(err).ShouldNot(HaveOccurred())

	gDbCtx.pool = pool
	gDbCtx.resource = resource

	var dbTemp *gorm.DB
	err = gDbCtx.pool.Retry(func() error {
		var er error

		dbTemp, er = gorm.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%s user=admin password=admin sslmode=disable", gDbCtx.GetPort()))
		return er
	})
	Expect(err).ShouldNot(HaveOccurred())
	dbTemp.Close()
}

func TerminateDBTest() {
	if os.Getenv("SKIP_UT_DB") != "" {
		return
	}
	Expect(gDbCtx.pool).ShouldNot(BeNil())
	err := gDbCtx.pool.Purge(gDbCtx.resource)
	Expect(err).ShouldNot(HaveOccurred())
	gDbCtx.pool = nil
}

func PrepareTestDB(dbName string, extrasSchemas ...interface{}) *gorm.DB {
	dbTemp, err := gorm.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%s user=admin password=admin sslmode=disable", gDbCtx.GetPort()))
	Expect(err).ShouldNot(HaveOccurred())
	defer dbTemp.Close()

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", strings.ToLower(dbName)))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s dbname=%s user=admin password=admin sslmode=disable", gDbCtx.GetPort(), strings.ToLower(dbName)))
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

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s user=admin password=admin sslmode=disable", gDbCtx.GetPort()))
	Expect(err).ShouldNot(HaveOccurred())
	defer db.Close()
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", strings.ToLower(dbName)))

	Expect(db.Error).ShouldNot(HaveOccurred())
}
