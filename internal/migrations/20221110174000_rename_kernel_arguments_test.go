package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("rename kernel arguments", func() {
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

	It("Migrates down and up", func() {
		Expect(gm.MigrateTo(renameKernelArgumentsID)).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeFalse())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeTrue())

		Expect(gm.RollbackMigration(renameKernelArguments())).ToNot(HaveOccurred())

		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeTrue())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeFalse())

		Expect(gm.MigrateTo(renameKernelArgumentsID)).ToNot(HaveOccurred())

		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeFalse())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeTrue())
	})

	It("both columns exist", func() {
		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeFalse())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeTrue())

		Expect(db.Exec("ALTER TABLE infra_envs ADD discovery_kernel_arguments TEXT").Error).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeTrue())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeTrue())

		Expect(gm.MigrateTo(renameKernelArgumentsID)).ToNot(HaveOccurred())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "discovery_kernel_arguments")).To(BeFalse())
		Expect(db.Migrator().HasColumn(&infraEnv{}, "kernel_arguments")).To(BeTrue())
	})
})
