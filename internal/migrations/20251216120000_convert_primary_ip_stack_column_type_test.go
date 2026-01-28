package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("convertPrimaryIPStackColumnType", func() {
	var (
		db        *gorm.DB
		dbName    string
		migration *gormigrate.Migration = convertPrimaryIPStackColumnType()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("should convert primary_ip_stack column from text to integer", func() {
		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Verify the column type before migration
		colType, err := getColumnType(dbName, &common.Cluster{}, "primary_ip_stack")
		Expect(err).ToNot(HaveOccurred())
		Expect(colType).To(Equal("text"))

		// Run all migrations up to but not including this one
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify the column type after migration
		// Note: PostgreSQL returns "int4" as the internal type name for INTEGER
		colType, err = getColumnType(dbName, &common.Cluster{}, "primary_ip_stack")
		Expect(err).ToNot(HaveOccurred())
		Expect(colType).To(Equal("int4"))
	})

	It("should convert 'ipv4' string value to integer 4", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Insert a cluster with text 'ipv4' value using raw SQL
		// (since the Go type is now int, we need to use raw SQL to insert the old text value)
		err := db.Exec(`
			INSERT INTO clusters (id, primary_ip_stack) 
			VALUES (?, 'ipv4')
		`, clusterID.String()).Error
		Expect(err).ToNot(HaveOccurred())

		// Run this migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify the value was converted to 4
		var primaryStack int
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal(4))
	})

	It("should convert 'ipv6' string value to integer 6", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Insert a cluster with text 'ipv6' value using raw SQL
		err := db.Exec(`
			INSERT INTO clusters (id, primary_ip_stack) 
			VALUES (?, 'ipv6')
		`, clusterID.String()).Error
		Expect(err).ToNot(HaveOccurred())

		// Run this migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify the value was converted to 6
		var primaryStack int
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal(6))
	})

	It("should preserve NULL values", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Insert a cluster with NULL primary_ip_stack (single-stack cluster)
		err := db.Exec(`
			INSERT INTO clusters (id, primary_ip_stack) 
			VALUES (?, NULL)
		`, clusterID.String()).Error
		Expect(err).ToNot(HaveOccurred())

		// Run this migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify the value remains NULL
		var primaryStack *int
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(BeNil())
	})

	It("should handle multiple clusters with mixed values", func() {
		clusterIPv4 := strfmt.UUID(uuid.New().String())
		clusterIPv6 := strfmt.UUID(uuid.New().String())
		clusterNull := strfmt.UUID(uuid.New().String())

		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Insert clusters with different values
		Expect(db.Exec(`INSERT INTO clusters (id, primary_ip_stack) VALUES (?, 'ipv4')`, clusterIPv4.String()).Error).ToNot(HaveOccurred())
		Expect(db.Exec(`INSERT INTO clusters (id, primary_ip_stack) VALUES (?, 'ipv6')`, clusterIPv6.String()).Error).ToNot(HaveOccurred())
		Expect(db.Exec(`INSERT INTO clusters (id, primary_ip_stack) VALUES (?, NULL)`, clusterNull.String()).Error).ToNot(HaveOccurred())

		// Run this migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify all values were converted correctly
		var stack4, stack6 int
		var stackNull *int

		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterIPv4).Select("primary_ip_stack").Scan(&stack4).Error).To(Succeed())
		Expect(stack4).To(Equal(4))

		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterIPv6).Select("primary_ip_stack").Scan(&stack6).Error).To(Succeed())
		Expect(stack6).To(Equal(6))

		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterNull).Select("primary_ip_stack").Scan(&stackNull).Error).To(Succeed())
		Expect(stackNull).To(BeNil())
	})

	It("should handle values '4' and '6' as integer values", func() {
		clusterIPv4 := strfmt.UUID(uuid.New().String())
		clusterIPv6 := strfmt.UUID(uuid.New().String())

		// Revert the column type back to text for upgrade testing
		Expect(revertColumnToText(db)).To(Succeed())

		// Insert clusters with different values
		Expect(db.Exec(`INSERT INTO clusters (id, primary_ip_stack) VALUES (?, '4')`, clusterIPv4.String()).Error).ToNot(HaveOccurred())
		Expect(db.Exec(`INSERT INTO clusters (id, primary_ip_stack) VALUES (?, '6')`, clusterIPv6.String()).Error).ToNot(HaveOccurred())

		// Run this migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify all values were converted correctly
		var stack4, stack6 int

		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterIPv4).Select("primary_ip_stack").Scan(&stack4).Error).To(Succeed())
		Expect(stack4).To(Equal(4))

		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterIPv6).Select("primary_ip_stack").Scan(&stack6).Error).To(Succeed())
		Expect(stack6).To(Equal(6))
	})

	It("should rollback from integer to text correctly", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert the column to text first (AutoMigrate creates it as integer)
		// so that the forward migration can run correctly
		Expect(revertColumnToText(db)).To(Succeed())

		// Run the migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Create a cluster with integer primary_ip_stack using the new type
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-rollback",
			},
			PrimaryIPStack: func() *common.PrimaryIPStack { v := common.PrimaryIPStackV4; return &v }(),
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run rollback of this specific migration
		Expect(gm.RollbackMigration(migration)).To(Succeed())

		// Verify the column type was reverted to text
		colType, err := getColumnType(dbName, &common.Cluster{}, "primary_ip_stack")
		Expect(err).ToNot(HaveOccurred())
		Expect(colType).To(Equal("text"))

		// Verify the value was converted back to 'ipv4'
		var primaryStack string
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal("ipv4"))
	})

	It("null values should be preserved during rollback", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert the column to text first (AutoMigrate creates it as integer)
		// so that the forward migration can run correctly
		Expect(revertColumnToText(db)).To(Succeed())

		// Run the migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Create a cluster with integer primary_ip_stack using the new type
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-rollback",
			},
			PrimaryIPStack: nil,
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run rollback of this specific migration
		Expect(gm.RollbackMigration(migration)).To(Succeed())

		// Verify the column type was reverted to text
		colType, err := getColumnType(dbName, &common.Cluster{}, "primary_ip_stack")
		Expect(err).ToNot(HaveOccurred())
		Expect(colType).To(Equal("text"))

		// Verify the value remains NULL
		var primaryStack *string
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(BeNil())
	})
})

// Function to revert the column type back to text for upgrade testing
func revertColumnToText(db *gorm.DB) error {
	return db.Exec(`
        ALTER TABLE clusters 
        ALTER COLUMN primary_ip_stack TYPE text
    `).Error
}
