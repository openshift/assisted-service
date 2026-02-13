package migrations

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("CreateFailedLoginAttempts Migration", func() {
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

	It("creates the failed_login_attempts table", func() {
		// Run the migration
		migration := createFailedLoginAttempts()
		err := migration.Migrate(db)
		Expect(err).ToNot(HaveOccurred())

		// Verify the table exists
		Expect(db.Migrator().HasTable("failed_login_attempts")).To(BeTrue())
	})

	It("can insert and query records", func() {
		// Run the migration
		migration := createFailedLoginAttempts()
		err := migration.Migrate(db)
		Expect(err).ToNot(HaveOccurred())

		// Insert a test record
		now := time.Now()
		attempt := FailedLoginAttempt{
			Identifier:     "testuser",
			IdentifierType: "username",
			AttemptCount:   3,
			FirstAttempt:   now,
			LastAttempt:    now,
		}
		err = db.Create(&attempt).Error
		Expect(err).ToNot(HaveOccurred())

		// Query the record
		var retrieved FailedLoginAttempt
		err = db.Where("identifier = ?", "testuser").First(&retrieved).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(retrieved.AttemptCount).To(Equal(3))
	})

	It("enforces unique constraint on identifier and identifier_type", func() {
		// Run the migration
		migration := createFailedLoginAttempts()
		err := migration.Migrate(db)
		Expect(err).ToNot(HaveOccurred())

		now := time.Now()
		attempt1 := FailedLoginAttempt{
			Identifier:     "testuser",
			IdentifierType: "username",
			AttemptCount:   1,
			FirstAttempt:   now,
			LastAttempt:    now,
		}
		err = db.Create(&attempt1).Error
		Expect(err).ToNot(HaveOccurred())

		// Try to insert duplicate
		attempt2 := FailedLoginAttempt{
			Identifier:     "testuser",
			IdentifierType: "username",
			AttemptCount:   2,
			FirstAttempt:   now,
			LastAttempt:    now,
		}
		err = db.Create(&attempt2).Error
		Expect(err).To(HaveOccurred()) // Should fail due to unique constraint
	})

	It("allows same identifier with different identifier_type", func() {
		// Run the migration
		migration := createFailedLoginAttempts()
		err := migration.Migrate(db)
		Expect(err).ToNot(HaveOccurred())

		now := time.Now()
		attempt1 := FailedLoginAttempt{
			Identifier:     "testuser",
			IdentifierType: "username",
			AttemptCount:   1,
			FirstAttempt:   now,
			LastAttempt:    now,
		}
		err = db.Create(&attempt1).Error
		Expect(err).ToNot(HaveOccurred())

		// Same identifier but different type
		attempt2 := FailedLoginAttempt{
			Identifier:     "testuser",
			IdentifierType: "ip",
			AttemptCount:   2,
			FirstAttempt:   now,
			LastAttempt:    now,
		}
		err = db.Create(&attempt2).Error
		Expect(err).ToNot(HaveOccurred())
	})

	It("can rollback the migration", func() {
		// Run the migration
		migration := createFailedLoginAttempts()
		err := migration.Migrate(db)
		Expect(err).ToNot(HaveOccurred())

		// Verify table exists
		Expect(db.Migrator().HasTable("failed_login_attempts")).To(BeTrue())

		// Rollback
		err = migration.Rollback(db)
		Expect(err).ToNot(HaveOccurred())

		// Verify table is gone
		Expect(db.Migrator().HasTable("failed_login_attempts")).To(BeFalse())
	})
})
