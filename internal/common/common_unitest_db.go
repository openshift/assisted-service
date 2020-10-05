package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/ory/dockertest/v3"
)

const (
	dbUser        = "postgres"
	dbPassword    = "admin"
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
		Repository: "quay.io/ocpmetal/postgresql-12-centos7",
		Tag:        "latest",
		Env: []string{
			fmt.Sprintf("POSTGRESQL_ADMIN_PASSWORD=%s", dbPassword),
			"POSTGRESQL_MAX_CONNECTION=10000",
		},
		Name: dbDockerName,
	})
	Expect(err).ShouldNot(HaveOccurred())

	gDbCtx.pool = pool
	gDbCtx.resource = resource

	var dbTemp *gorm.DB
	err = gDbCtx.pool.Retry(func() error {
		var er error
		dbTemp, er = dbConnect()
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
	dbTemp, err := dbConnect()
	Expect(err).ShouldNot(HaveOccurred())
	defer dbTemp.Close()

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", strings.ToLower(dbName)))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := dbConnectWithDB(dbName)
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
	db, err := dbConnect()
	Expect(err).ShouldNot(HaveOccurred())
	defer db.Close()
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", strings.ToLower(dbName)))

	Expect(db.Error).ShouldNot(HaveOccurred())
}

func dbConnect() (*gorm.DB, error) {
	return gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s user=%s password=%s sslmode=disable",
			gDbCtx.GetPort(), dbUser, dbPassword))
}

func dbConnectWithDB(dbName string) (*gorm.DB, error) {
	return gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s dbname=%s user=%s password=%s sslmode=disable",
			gDbCtx.GetPort(), strings.ToLower(dbName), dbUser, dbPassword))
}
