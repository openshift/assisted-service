package migrations

import (
	"fmt"
	"sort"
	"testing"

	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"
	gormigrate "gopkg.in/gormigrate.v1"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "Migrations Suite")
}

// migrateToBefore is a helper function for migration tests
// It runs all the migrations before the given one to simplify setting up a valid test scenario
// nolint,unused
func migrateToBefore(db *gorm.DB, migrationID string) error {
	allMigrations := post()

	id := sort.Search(len(allMigrations), func(i int) bool { return allMigrations[i].ID >= migrationID })
	if id == len(allMigrations) || allMigrations[id].ID != migrationID {
		return fmt.Errorf("Failed to find migration %s in migration list", migrationID)
	}

	toRun := allMigrations[0:id]
	if len(toRun) > 0 {
		return gormigrate.New(db, gormigrate.DefaultOptions, allMigrations[0:id]).Migrate()
	}

	return nil
}

// migrateTo runs all migrations up to and including migrationID
// nolint,unused
func migrateTo(db *gorm.DB, migratoinID string) error {
	gm := gormigrate.New(db, gormigrate.DefaultOptions, post())
	return gm.MigrateTo(migratoinID)
}

func getColumnType(db *gorm.DB, model interface{}, column string) (string, error) {
	rows, err := db.Model(model).Rows()
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		Expect(rows.Close()).To(Succeed())
	}()

	colTypes, err := rows.ColumnTypes()
	Expect(err).NotTo(HaveOccurred())

	for _, colType := range colTypes {
		if colType.Name() == column {
			return colType.DatabaseTypeName(), nil
		}
	}
	return "", errors.Errorf("Failed to find %s column in %T", column, model)
}
