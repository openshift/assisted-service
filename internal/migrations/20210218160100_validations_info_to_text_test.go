package migrations

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/dbc"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/gormigrate.v1"
)

const clusterValidationsInfo = `{"configuration":[{"id":"pull-secret-set","status":"success","message":"The pull secret is set."}]}`

var _ = Describe("ChangeValidationsInfoToText", func() {
	var (
		db        *gorm.DB
		dbName    string
		gm        *gormigrate.Gormigrate
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = dbc.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := dbc.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			ValidationsInfo: clusterValidationsInfo,
		}}
		err := db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		gm = gormigrate.New(db, gormigrate.DefaultOptions, all())
		err = gm.MigrateTo("20210218160100")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		dbc.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(db, &dbc.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectValidationsInfo(db, clusterID.String(), clusterValidationsInfo)

		err = gm.RollbackMigration(changeClusterValidationsInfoToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &dbc.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("VARCHAR"))
		expectValidationsInfo(db, clusterID.String(), clusterValidationsInfo)

		err = gm.MigrateTo("20210218160100")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &dbc.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectValidationsInfo(db, clusterID.String(), clusterValidationsInfo)
	})
})

func expectValidationsInfo(db *gorm.DB, clusterID string, validationsInfo string) {
	var c dbc.Cluster
	err := db.First(&c, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.ValidationsInfo).To(Equal(validationsInfo))
}
