package migrations

import (
	"strings"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
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

		overrides = `{"networking":{"networkType": "OVNKubernetes"},"fips":true}`
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterID,
			InstallConfigOverrides: overrides,
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

	FIt("Migrates down and up", func() {
		t, err := getColumnType(dbName, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectOverride(dbName, clusterID.String(), overrides)

		err = gm.RollbackMigration(changeOverridesToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("VARCHAR"))
		expectOverride(dbName, clusterID.String(), overrides)

		err = gm.MigrateTo("20201019194303")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "install_config_overrides")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))
		expectOverride(dbName, clusterID.String(), overrides)
	})
})

func expectOverride(dbName string, clusterID string, override string) {
	var c common.Cluster
	db, err := common.OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())
	defer common.CloseDB(db)
	err = db.First(&c, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.InstallConfigOverrides).To(Equal(override))
}
