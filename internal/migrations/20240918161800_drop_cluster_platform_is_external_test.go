package migrations

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("dropClusterPlatformIsExternal", func() {
	var (
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("succeeds when the platform_is_external column is present", func() {
		Expect(migrateToBefore(db, "20240918161800")).To(Succeed())

		Expect(db.Exec("ALTER TABLE clusters ADD COLUMN IF NOT EXISTS platform_is_external BOOLEAN").Error).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn("clusters", "platform_is_external")).To(BeTrue())

		Expect(migrateTo(db, "20240918161800")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "platform_is_external")).To(BeFalse())
	})

	It("succeeds when the platform_is_external column is not present", func() {
		Expect(migrateToBefore(db, "20240918161800")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "platform_is_external")).To(BeFalse())

		Expect(migrateTo(db, "20240918161800")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "platform_is_external")).To(BeFalse())
	})
})
