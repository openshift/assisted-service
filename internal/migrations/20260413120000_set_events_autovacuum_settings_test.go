package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("set events autovacuum settings", func() {
	var (
		db     *gorm.DB
		dbName string
		gm     *gormigrate.Gormigrate
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	getTableOptions := func() (string, string) {
		var vacuumScaleFactor, analyzeScaleFactor string
		err := db.Raw(`
			SELECT
				COALESCE((SELECT option_value FROM pg_options_to_table(reloptions) WHERE option_name = 'autovacuum_vacuum_scale_factor'), '') as vacuum_scale_factor,
				COALESCE((SELECT option_value FROM pg_options_to_table(reloptions) WHERE option_name = 'autovacuum_analyze_scale_factor'), '') as analyze_scale_factor
			FROM pg_class
			WHERE relname = 'events'
		`).Row().Scan(&vacuumScaleFactor, &analyzeScaleFactor)
		Expect(err).ToNot(HaveOccurred())
		return vacuumScaleFactor, analyzeScaleFactor
	}

	It("Migrates down and up", func() {
		// Initial state - no custom settings
		vacuumBefore, analyzeBefore := getTableOptions()
		Expect(vacuumBefore).To(BeEmpty())
		Expect(analyzeBefore).To(BeEmpty())

		// Migrate forward
		Expect(gm.MigrateTo(setEventsAutovacuumSettingsID)).ToNot(HaveOccurred())

		vacuumAfter, analyzeAfter := getTableOptions()
		Expect(vacuumAfter).To(Equal("0.05"))
		Expect(analyzeAfter).To(Equal("0.025"))

		// Rollback
		Expect(gm.RollbackMigration(setEventsAutovacuumSettings())).ToNot(HaveOccurred())

		vacuumRolledBack, analyzeRolledBack := getTableOptions()
		Expect(vacuumRolledBack).To(BeEmpty())
		Expect(analyzeRolledBack).To(BeEmpty())

		// Migrate forward again
		Expect(gm.MigrateTo(setEventsAutovacuumSettingsID)).ToNot(HaveOccurred())

		vacuumFinal, analyzeFinal := getTableOptions()
		Expect(vacuumFinal).To(Equal("0.05"))
		Expect(analyzeFinal).To(Equal("0.025"))
	})
})
