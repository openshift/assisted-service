package migrations

import (
	"database/sql"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"gorm.io/gorm"
)

var _ = Describe("createInfraEnvImageTokenKey", func() {
	var (
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		err := migrateToBefore(db, "20211108163809")
		Expect(err).ToNot(HaveOccurred())

		nilKeyID := strfmt.UUID(uuid.New().String())
		err = db.Exec("INSERT INTO infra_envs (id, image_token_key) VALUES (?, ?)", nilKeyID, sql.NullString{Valid: false}).Error
		Expect(err).NotTo(HaveOccurred())

		emptyKeyID := strfmt.UUID(uuid.New().String())
		err = db.Exec("INSERT INTO infra_envs (id, image_token_key) VALUES (?, ?)", emptyKeyID, sql.NullString{Valid: true, String: ""}).Error
		Expect(err).NotTo(HaveOccurred())

		setKeyID := strfmt.UUID(uuid.New().String())
		err = db.Exec("INSERT INTO infra_envs (id, image_token_key) VALUES (?, ?)", setKeyID, sql.NullString{Valid: true, String: "key"}).Error
		Expect(err).NotTo(HaveOccurred())

		err = migrateTo(db, "20211108163809")
		Expect(err).NotTo(HaveOccurred())

		var nilKeyEnv common.InfraEnv
		err = db.First(&nilKeyEnv, "id = ?", nilKeyID).Error
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nilKeyEnv.ImageTokenKey)).NotTo(BeZero())

		var emptyKeyEnv common.InfraEnv
		err = db.First(&emptyKeyEnv, "id = ?", emptyKeyID).Error
		Expect(err).NotTo(HaveOccurred())
		Expect(len(emptyKeyEnv.ImageTokenKey)).NotTo(BeZero())

		var setKeyEnv common.InfraEnv
		err = db.First(&setKeyEnv, "id = ?", setKeyID).Error
		Expect(err).NotTo(HaveOccurred())
		Expect(setKeyEnv.ImageTokenKey).To(Equal("key"))
	})
})
