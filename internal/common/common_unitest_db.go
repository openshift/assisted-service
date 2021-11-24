package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/ory/dockertest/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
		Repository: "quay.io/edge-infrastructure/postgresql-12-centos7",
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

		dbTemp, er = openTopTestDBConn()
		return er
	})
	Expect(err).ShouldNot(HaveOccurred())
	CloseDB(dbTemp)
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
	dbTemp, err := openTopTestDBConn()
	Expect(err).ShouldNot(HaveOccurred())
	defer CloseDB(dbTemp)

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbName))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())
	// db = db.Debug()
	err = AutoMigrate(db)
	Expect(err).ShouldNot(HaveOccurred())

	if len(extrasSchemas) > 0 {
		for _, schema := range extrasSchemas {
			Expect(db.AutoMigrate(schema)).ToNot(HaveOccurred())
		}
	}
	return db, dbName
}

func DeleteTestDB(db *gorm.DB, dbName string) {
	CloseDB(db)

	db, err := openTopTestDBConn()
	Expect(err).ShouldNot(HaveOccurred())
	defer CloseDB(db)
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbName))

	Expect(db.Error).ShouldNot(HaveOccurred())
}

func openTestDB(dbName string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=127.0.0.1 port=%s user=postgres password=admin sslmode=disable", gDbCtx.GetPort())
	if dbName != "" {
		dsn = dsn + fmt.Sprintf(" database=%s", dbName)
	}
	return gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
}

func openTopTestDBConn() (*gorm.DB, error) {
	return openTestDB("")
}

func OpenTestDBConn(dbName string) (*gorm.DB, error) {
	return openTestDB(dbName)
}
