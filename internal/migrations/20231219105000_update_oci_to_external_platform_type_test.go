package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

type platformResults struct {
	PlatformType                           *string
	PlatformExternalCloudControllerManager *string
	PlatformExternalPlatformName           *string
}

var _ = Describe("updateOciToExternalPlatformType", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID = strfmt.UUID("46a8d745-dfce-4fd8-9df0-549ee8eabb3d")
		gm        *gormigrate.Gormigrate
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		var err error

		err = migrateToBefore(db, "20231219105000")
		Expect(err).NotTo(HaveOccurred())

		// create cluster with platform_type = oci
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
			},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		err = db.Exec("UPDATE clusters SET platform_type=? WHERE id=?", "oci", clusterID).Error
		Expect(err).NotTo(HaveOccurred())

		err = migrateTo(db, "20231219105000")
		Expect(err).NotTo(HaveOccurred())

		// test
		var res platformResults
		db.Raw("SELECT platform_type, platform_external_platform_name, platform_external_cloud_controller_manager FROM clusters WHERE id=?", clusterID).Scan(&res)
		Expect(*res.PlatformType).To(Equal("external"))
		Expect(*res.PlatformExternalPlatformName).To(Equal("oci"))
		Expect(*res.PlatformExternalCloudControllerManager).To(Equal("External"))
	})

	It("Migrates down", func() {
		err := migrateTo(db, "20231219105000")
		Expect(err).NotTo(HaveOccurred())

		// create cluster with platform_type = oci
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("oci"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
					},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		Expect(gm.RollbackMigration(updateOciToExternalPlatformType())).ToNot(HaveOccurred())

		// test
		var res platformResults
		db.Raw("SELECT platform_type, platform_external_platform_name, platform_external_cloud_controller_manager FROM clusters WHERE id=?", clusterID).Scan(&res)
		Expect(*res.PlatformType).To(Equal("oci"))
		Expect(res.PlatformExternalPlatformName).To(BeNil())
		Expect(res.PlatformExternalCloudControllerManager).To(BeNil())
	})
})
