package migrations

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	gormigrate "gopkg.in/gormigrate.v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("changeOverridesToText", func() {
	var (
		db        *gorm.DB
		gm        *gormigrate.Gormigrate
		clusterID strfmt.UUID
		overrides string
	)

	BeforeEach(func() {
		db = common.PrepareTestDB("change_overrides_to_text", &events.Event{})

		overrides = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:                      &clusterID,
			IgnitionConfigOverrides: overrides,
		}}
		err := db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		gm = gormigrate.New(db, gormigrate.DefaultOptions, all())
		err = gm.MigrateTo("20201019194303")
		Expect(err).ToNot(HaveOccurred())
	})

	It("Migrates down and up", func() {
		t, err := columnType(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectOverride(db, clusterID.String(), overrides)

		err = gm.RollbackMigration(changeOverridesToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = columnType(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("VARCHAR"))
		expectOverride(db, clusterID.String(), overrides)

		err = gm.MigrateTo("20201019194303")
		Expect(err).ToNot(HaveOccurred())

		t, err = columnType(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))
		expectOverride(db, clusterID.String(), overrides)
	})
})

func columnType(db *gorm.DB) (string, error) {
	rows, err := db.Model(&common.Cluster{}).Rows()
	Expect(err).NotTo(HaveOccurred())

	colTypes, err := rows.ColumnTypes()
	Expect(err).NotTo(HaveOccurred())

	for _, colType := range colTypes {
		if colType.Name() == "install_config_overrides" {
			return colType.DatabaseTypeName(), nil
		}
	}
	return "", errors.New("Failed to find install_config_overrides column in clusters")
}

func expectOverride(db *gorm.DB, clusterID string, override string) {
	var c common.Cluster
	err := db.First(&c, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	Expect(c.IgnitionConfigOverrides).To(Equal(override))
}
