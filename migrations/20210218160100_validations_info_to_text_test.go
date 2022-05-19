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

const clusterValidationsInfo = `{"configuration":[{"id":"pull-secret-set","status":"success","message":"The pull secret is set."}]}`

var _ = Describe("ChangeValidationsInfoToText", func() {
	var (
		db        *gorm.DB
		dbName    string
		gm        *gormigrate.Gormigrate
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			ValidationsInfo: clusterValidationsInfo,
		}}
		err := db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
		err = gm.MigrateTo("20210218160100")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(dbName, &common.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectValidationsInfo(dbName, clusterID.String(), clusterValidationsInfo)

		err = gm.RollbackMigration(changeClusterValidationsInfoToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("VARCHAR"))
		expectValidationsInfo(dbName, clusterID.String(), clusterValidationsInfo)

		err = gm.MigrateTo("20210218160100")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "validations_info")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectValidationsInfo(dbName, clusterID.String(), clusterValidationsInfo)
	})
})

func expectValidationsInfo(dbName string, clusterID string, validationsInfo string) {
	var c common.Cluster
	db, err := common.OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())
	defer common.CloseDB(db)
	err = db.First(&c, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.ValidationsInfo).To(Equal(validationsInfo))
}
