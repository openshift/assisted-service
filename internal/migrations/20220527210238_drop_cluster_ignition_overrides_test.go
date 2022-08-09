package migrations

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("dropClusterIgnitionOverrides", func() {
	var (
		db *gorm.DB
	)

	BeforeEach(func() {
		db, _ = common.PrepareTestDB()
	})

	It("succeeds when the column is present", func() {
		Expect(migrateToBefore(db, "20220527210238")).To(Succeed())

		Expect(db.Exec("ALTER TABLE clusters ADD COLUMN IF NOT EXISTS ignition_config_overrides TEXT").Error).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn("clusters", "ignition_config_overrides")).To(BeTrue())

		Expect(migrateTo(db, "20220527210238")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ignition_config_overrides")).To(BeFalse())
	})

	It("succeeds when the column is not present", func() {
		Expect(migrateToBefore(db, "20220527210238")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ignition_config_overrides")).To(BeFalse())

		Expect(migrateTo(db, "20220527210238")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ignition_config_overrides")).To(BeFalse())
	})
})
