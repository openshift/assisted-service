package migrations

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("dropClusterApiVipAndIngressVip", func() {
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

	It("succeeds when the api_vip column is present", func() {
		Expect(migrateToBefore(db, "20231010101010")).To(Succeed())

		Expect(db.Exec("ALTER TABLE clusters ADD COLUMN IF NOT EXISTS api_vip TEXT").Error).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn("clusters", "api_vip")).To(BeTrue())

		Expect(migrateTo(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "api_vip")).To(BeFalse())
	})

	It("succeeds when the ingress_vip column is present", func() {
		Expect(migrateToBefore(db, "20231010101010")).To(Succeed())
		Expect(db.Exec("ALTER TABLE clusters ADD COLUMN IF NOT EXISTS ingress_vip TEXT").Error).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn("clusters", "ingress_vip")).To(BeTrue())

		Expect(migrateTo(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ingress_vip")).To(BeFalse())
	})

	It("succeeds when the api_vip column is not present", func() {
		Expect(migrateToBefore(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "api_vip")).To(BeFalse())

		Expect(migrateTo(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "api_vip")).To(BeFalse())
	})

	It("succeeds when the ingress_vip column is not present", func() {
		Expect(migrateToBefore(db, "20231010101010")).To(Succeed())

		Expect(migrateToBefore(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ingress_vip")).To(BeFalse())

		Expect(migrateTo(db, "20231010101010")).To(Succeed())
		Expect(db.Migrator().HasColumn("clusters", "ingress_vip")).To(BeFalse())
	})
})
