package migrations

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	gormigrate "gopkg.in/gormigrate.v1"
)

var _ = Describe("changeOverridesToText", func() {
	var (
		db        *gorm.DB
		dbName    string
		gm        *gormigrate.Gormigrate
		clusterID strfmt.UUID
		overrides string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		overrides = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:                      &clusterID,
			IgnitionConfigOverrides: overrides,
		}}
		err := db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
		err = gm.MigrateTo("20201019194303")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(db, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectOverride(db, clusterID.String(), overrides)

		err = gm.RollbackMigration(changeOverridesToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("VARCHAR"))
		expectOverride(db, clusterID.String(), overrides)

		err = gm.MigrateTo("20201019194303")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectOverride(db, clusterID.String(), overrides)
	})
})

func expectOverride(db *gorm.DB, clusterID string, override string) {
	var c common.Cluster
	err := db.First(&c, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.IgnitionConfigOverrides).To(Equal(override))
}
