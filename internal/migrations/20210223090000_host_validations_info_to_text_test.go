package migrations

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/gormigrate.v1"
)

const hostValidationsInfo = `{"operators":[{"id":"ocs-requirements-satisfied","status":"success","message":"ocs is disabled"}]}`

var _ = Describe("ChangeHostValidationsInfoToText", func() {
	var (
		db     *gorm.DB
		dbName string
		gm     *gormigrate.Gormigrate
		hostID strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		hostID = strfmt.UUID(uuid.New().String())
		clusterID := strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID:              &hostID,
			ValidationsInfo: hostValidationsInfo,
			ClusterID:       clusterID,
			InfraEnvID:      clusterID,
		}
		err := db.Create(&host).Error
		Expect(err).NotTo(HaveOccurred())

		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
		err = gm.MigrateTo("20210223090000")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(db, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectHostValidationsInfo(db, hostID.String(), hostValidationsInfo)

		err = gm.RollbackMigration(changeHostValidationsInfoToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("VARCHAR"))
		expectHostValidationsInfo(db, hostID.String(), hostValidationsInfo)

		err = gm.MigrateTo("20210223090000")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectHostValidationsInfo(db, hostID.String(), hostValidationsInfo)
	})
})

func expectHostValidationsInfo(db *gorm.DB, hostID string, validationsInfo string) {
	var c models.Host
	err := db.First(&c, "id = ?", hostID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.ValidationsInfo).To(Equal(validationsInfo))
}
