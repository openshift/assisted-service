package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var (
	id1 = strfmt.UUID(uuid.New().String())
	id2 = strfmt.UUID(uuid.New().String())
	id3 = strfmt.UUID(uuid.New().String())
	id4 = strfmt.UUID(uuid.New().String())
)

var _ = Describe("updateNewColumnControlPlaneCountValueForExistingSNOClusterRecords", func() {
	var (
		db        *gorm.DB
		dbName    string
		migration *gormigrate.Migration = updateNewColumnControlPlaneCountValueForExistingClusterRecords()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("succeeds", func() {
		It("changing only records with control_plane_count = 0", func() {
			clusters := []*common.Cluster{
				{
					Cluster: models.Cluster{
						ID:                   &id1,
						HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
						ControlPlaneCount:    0,
					},
				},
				{
					Cluster: models.Cluster{
						ID:                   &id2,
						HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
						ControlPlaneCount:    1,
					},
				},
				{
					Cluster: models.Cluster{
						ID:                   &id3,
						HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
						ControlPlaneCount:    0,
					},
				},
				{
					Cluster: models.Cluster{
						ID:                   &id4,
						HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
						ControlPlaneCount:    3,
					},
				},
			}

			Expect(db.Create(&clusters).Error).To(Succeed())
			Expect(migrateToBefore(db, migration.ID)).To(Succeed())
			Expect(migrateTo(db, migration.ID)).To(Succeed())

			var count int64

			Expect(db.Model(&common.Cluster{}).Where("control_plane_count = ?", 0).Count(&count).Error).To(Succeed())
			Expect(count).To(BeEquivalentTo(0))

			Expect(db.Model(&common.Cluster{}).Where("control_plane_count = ?", 1).Count(&count).Error).To(Succeed())
			Expect(count).To(BeEquivalentTo(2))

			Expect(db.Model(&common.Cluster{}).Where("control_plane_count = ?", 3).Count(&count).Error).To(Succeed())
			Expect(count).To(BeEquivalentTo(2))
		})
	})

	It("changing only records with control_plane_count = NULL", func() {
		Expect(
			db.Exec(
				"INSERT INTO clusters (id, control_plane_count, high_availability_mode) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?), (?, ?, ?)",
				id1, nil, models.ClusterCreateParamsHighAvailabilityModeNone,
				id2, 1, models.ClusterCreateParamsHighAvailabilityModeNone,
				id3, 3, models.ClusterCreateParamsHighAvailabilityModeFull,
				id4, nil, models.ClusterCreateParamsHighAvailabilityModeFull,
			).Error,
		).To(Succeed())
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		var count int64

		Expect(db.Model(&common.Cluster{}).Where("control_plane_count IS NULL").Count(&count).Error).To(Succeed())
		Expect(count).To(BeEquivalentTo(0))

		Expect(db.Model(&common.Cluster{}).Where("control_plane_count = ?", 1).Count(&count).Error).To(Succeed())
		Expect(count).To(BeEquivalentTo(2))

		Expect(db.Model(&common.Cluster{}).Where("control_plane_count = ?", 3).Count(&count).Error).To(Succeed())
		Expect(count).To(BeEquivalentTo(2))
	})

	It("with no existing records", func() {
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		var count int64

		Expect(db.Model(&common.Cluster{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeEquivalentTo(0))
	})
})
