package migrations

import (
	"strings"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

const hostValidationsInfo = `{"operators":[{"id":"odf-requirements-satisfied","status":"success","message":"odf is disabled"}]}`

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
			ClusterID:       &clusterID,
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
		t, err := getColumnType(dbName, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectHostValidationsInfo(dbName, hostID.String(), hostValidationsInfo)

		err = gm.RollbackMigration(changeHostValidationsInfoToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("VARCHAR"))
		expectHostValidationsInfo(dbName, hostID.String(), hostValidationsInfo)

		err = gm.MigrateTo("20210223090000")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &models.Host{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectHostValidationsInfo(dbName, hostID.String(), hostValidationsInfo)
	})
})

func expectHostValidationsInfo(dbName string, hostID string, validationsInfo string) {
	var c models.Host
	db, err := common.OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())
	defer common.CloseDB(db)
	err = db.First(&c, "id = ?", hostID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.ValidationsInfo).To(Equal(validationsInfo))
}
