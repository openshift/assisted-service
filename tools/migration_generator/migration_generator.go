package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/iancoleman/strcase"
)

func main() {
	var migrationName string
	flag.StringVar(&migrationName, "name", "", "The migration name to use")
	flag.Parse()

	if migrationName == "" {
		println("A migration name must be specified")
		flag.PrintDefaults()
	}

	now := time.Now().UTC()
	id := now.Format("20060102150405")
	funcName := strcase.ToLowerCamel(migrationName)
	funcNameSnake := strcase.ToSnake(migrationName)

	filePath := filepath.Join("internal/migrations", fmt.Sprintf("%s_%s.go", id, funcNameSnake))
	testFilePath := filepath.Join("internal/migrations", fmt.Sprintf("%s_%s_test.go", id, funcNameSnake))

	data := struct {
		ID       string
		FuncName string
	}{
		ID:       id,
		FuncName: funcName,
	}

	file, err := os.Create(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create migration file %s: %s", filePath, err)
		os.Exit(1)
	}

	testFile, err := os.Create(testFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create migration test file %s: %s", testFilePath, err)
		os.Exit(1)
	}

	err = template.Must(template.New("migration").Parse(migrationTemplate)).Execute(file, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write migration file %s: %s", filePath, err)
		os.Exit(1)
	}

	err = template.Must(template.New("migrationTest").Parse(migrationTestTemplate)).Execute(testFile, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write migration file %s: %s", testFilePath, err)
		os.Exit(1)
	}
}

var migrationTemplate = `package migrations

import (
	"gorm.io/gorm"
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
)

func {{.FuncName}}() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		// TODO
		return nil
	}

	rollback := func(tx *gorm.DB) error {
		// TODO
		return nil
	}

	return &gormigrate.Migration{
		ID:       "{{.ID}}",
		Migrate:  gormigrate.MigrateFunc(migrate),
		Rollback: gormigrate.RollbackFunc(rollback),
	}
}
`

var migrationTestTemplate = `package migrations

import (
	"github.com/openshift/assisted-service/internal/common"

	"gorm.io/gorm"
	gormigrate "github.com/go-gormigrate/gormigrate/v2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("{{.FuncName}}", func() {
	var (
		db        *gorm.DB
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		err := migrateToBefore(db, "{{.ID}}")
		Expect(err).ToNot(HaveOccurred())

		// setup

		err = migrateTo(db, "{{.ID}}")
		Expect(err).NotTo(HaveOccurred())

		// test
	})

	It("Migrates down", func() {
		err := migrateTo(db, "{{.ID}}")
		Expect(err).NotTo(HaveOccurred())

		// setup

		err = gormigrate.New(db, gormigrate.DefaultOptions, post()).RollbackMigration({{.FuncName}}())
		Expect(err).NotTo(HaveOccurred())

		// test
	})
})
`
