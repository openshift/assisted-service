package common

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Singleton initialized and returned by getDatabaseContext
var globalDatabaseContext DBContext

// Unit-test database connection handling. This supports automatically running Postgres in:
// - When SKIP_UT_DB env var is set, database is assumed to already be up and available at localhost:5432.
// - Otherwise, an attempt is made to launch postgres on the current k8s cluster defined by kubeconfig
// - If that fails, we try to run it as a local podman container
// - If that fails, we try to run it as a local docker container

const (
	databaseContainerImage    = "quay.io/centos7/postgresql-12-centos7"
	databaseDataDir           = "/var/lib/pgsql/data"
	databaseContainerImageTag = "latest"
	databaseAdminPassword     = "admin"

	// This has to match the port in `podman inspect [databaseContainerImage] | jq .[].Config.ExposedPorts`
	databaseDefaultPort = 5432
)

// DBContext is an interface for the various DB implementations
type DBContext interface {
	GetDatabaseHostPort() (string, string)
	RunDatabase() error
	TeardownDatabase()
}

func generateUniqueContainerName() string {
	uuid, err := uuid.NewUUID()
	Expect(err).ShouldNot(HaveOccurred())
	containerNameUUID := uuid.String()
	return fmt.Sprintf("assisted-service-unittest-database-%s", containerNameUUID)
}

func getDatabaseContext() DBContext {
	if globalDatabaseContext == nil {
		if os.Getenv("SKIP_UT_DB") != "" {
			// The user is telling us the database should already be available locally
			globalDatabaseContext = &LocalDBContext{}
		} else {
			// Database has to be launched

			// Try k8s first
			k8sDBContext, err := getKubernetesDBContext()
			if err == nil && k8sDBContext.RunDatabase() == nil {
				globalDatabaseContext = k8sDBContext
			} else {
				fmt.Printf("Failed to launch database container with k8s: %s\nTrying regular containers...\n", err)
				containerName := generateUniqueContainerName()

				podmanDBContext, err := getPodmanDBContext(containerName)
				if err == nil {
					err = podmanDBContext.RunDatabase()
					if err == nil {
						globalDatabaseContext = podmanDBContext
					} else {
						fmt.Printf("Failed to launch database container with podman: %s\nTrying docker...\n", err)
						// podman failed, try running in a docker container
						dockerDBContext, err := getDockerDBContext(containerName)
						Expect(err).ShouldNot(HaveOccurred())

						err = dockerDBContext.RunDatabase()
						Expect(err).ShouldNot(HaveOccurred())

						globalDatabaseContext = dockerDBContext
					}
				}
			}
		}

		host, port := globalDatabaseContext.GetDatabaseHostPort()
		switch globalDatabaseContext.(type) {
		case *LocalDBContext:
			fmt.Printf("Assuming database is already available at %s:%s\n", host, port)
		case *KubernetesDBContext:
			fmt.Printf("Launched database on k8s cluster at %s:%s\n", host, port)
		case *DockerDBContext:
			fmt.Printf("Launched database with Docker running at %s:%s\n", host, port)
		}
	}

	return globalDatabaseContext
}

func InitializeTestDatabase() {
	RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})

	defer func() {
		panicError := recover()
		if panicError != nil {
			fmt.Println("Panic during database initialization:")
			fmt.Printf("Failure during database setup: %s\n", panicError)
			os.Exit(1)
		}
	}()

	var dbTemp *gorm.DB
	dbTemp, _ = openTopTestDBConn()
	CloseDB(dbTemp)
}

func TerminateTestDatabase() {
	getDatabaseContext().TeardownDatabase()
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
	Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("failed to connect to unit-test database at DSN: %s", getDatabaseDSN("")))
	defer CloseDB(dbTemp)

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbName))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())

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

func getDatabaseDSN(dbName string) string {
	host, port := getDatabaseContext().GetDatabaseHostPort()
	dsn := fmt.Sprintf("host=%s port=%s user=postgres password=%s sslmode=disable", host, port, databaseAdminPassword)
	if dbName != "" {
		dsn = dsn + fmt.Sprintf(" database=%s", dbName)
	}
	return dsn
}

func openTestDB(dbName string) (*gorm.DB, error) {
	dbDSN := getDatabaseDSN(dbName)

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second, // Slow SQL threshold
			IgnoreRecordNotFoundError: true,        // Ignore ErrRecordNotFound error for logger
		},
	)
	return gorm.Open(postgres.Open(dbDSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   newLogger,
	})
}

func openTopTestDBConn() (*gorm.DB, error) {
	return openTestDB("")
}

func OpenTestDBConn(dbName string) (*gorm.DB, error) {
	return openTestDB(dbName)
}
