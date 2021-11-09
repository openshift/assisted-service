package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/gomega"
	"github.com/ory/dockertest/v3"
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
		Repository: "postgresql-12-centos7",
		Tag:        "latest",
		Env:        []string{"POSTGRESQL_ADMIN_PASSWORD=admin"},
		Name:       dbDockerName,
	})
	Expect(err).ShouldNot(HaveOccurred())

	gDbCtx.pool = pool
	gDbCtx.resource = resource

	var dbTemp *gorm.DB
	err = gDbCtx.pool.Retry(func() error {
		var er error

		dbTemp, er = gorm.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%s user=postgres password=admin sslmode=disable", gDbCtx.GetPort()))
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

// Creates a valid postgresql db name from a random uuid
// DB names (and all identifiers) must begin with a letter or '_'
// Additionally using underscores rather than hyphens reduces the chance of quoting bugs
func randomDBName() string {
	return fmt.Sprintf("_%s", strings.ReplaceAll(uuid.New().String(), "-", "_"))
}

func PrepareTestDB(extrasSchemas ...interface{}) (*gorm.DB, string) {
	dbName := randomDBName()
	dbTemp, err := gorm.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%s user=postgres password=admin sslmode=disable", gDbCtx.GetPort()))
	Expect(err).ShouldNot(HaveOccurred())
	defer dbTemp.Close()

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbName))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s dbname=%s user=postgres password=admin sslmode=disable", gDbCtx.GetPort(), dbName))
	Expect(err).ShouldNot(HaveOccurred())
	// db = db.Debug()
	err = AutoMigrate(db)
	Expect(err).ShouldNot(HaveOccurred())

	if len(extrasSchemas) > 0 {
		for _, schema := range extrasSchemas {
			db.AutoMigrate(schema)
			Expect(db.Error).ShouldNot(HaveOccurred())
		}
	}
	return db, dbName
}

func DeleteTestDB(db *gorm.DB, dbName string) {
	db.Close()

	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=127.0.0.1 port=%s user=postgres password=admin sslmode=disable", gDbCtx.GetPort()))
	Expect(err).ShouldNot(HaveOccurred())
	defer db.Close()
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbName))

	Expect(db.Error).ShouldNot(HaveOccurred())
}
