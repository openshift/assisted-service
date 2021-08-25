package migrations

import (
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("copyClusterUserInfoToInfraEnv", func() {
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

	createClusterAndInfraEnv := func(num int) strfmt.UUID {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:          &clusterID,
			UserName:    fmt.Sprintf("user%d@example.com", num),
			EmailDomain: "example.com",
			OrgID:       fmt.Sprintf("123%d", num),
		}}
		err := db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv := &common.InfraEnv{InfraEnv: models.InfraEnv{
			ClusterID: clusterID,
			ID:        infraEnvID,
		}}

		err = db.Create(&infraEnv).Error
		Expect(err).NotTo(HaveOccurred())

		return infraEnvID
	}

	It("sets the user info in the infra env", func() {
		err := migrateToBefore(db, "20210825135341")
		Expect(err).ToNot(HaveOccurred())

		id1 := createClusterAndInfraEnv(1)
		id2 := createClusterAndInfraEnv(2)

		err = migrateTo(db, "20210825135341")
		Expect(err).NotTo(HaveOccurred())

		var infraEnv1 common.InfraEnv
		err = db.First(&infraEnv1, "id = ?", id1).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(infraEnv1.UserName).To(Equal("user1@example.com"))
		Expect(infraEnv1.EmailDomain).To(Equal("example.com"))
		Expect(infraEnv1.OrgID).To(Equal("1231"))

		var infraEnv2 common.InfraEnv
		err = db.First(&infraEnv2, "id = ?", id2).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(infraEnv2.UserName).To(Equal("user2@example.com"))
		Expect(infraEnv2.EmailDomain).To(Equal("example.com"))
		Expect(infraEnv2.OrgID).To(Equal("1232"))
	})

	It("doesn't overwrite existing infra env user info", func() {
		err := migrateToBefore(db, "20210825135341")
		Expect(err).ToNot(HaveOccurred())

		id := createClusterAndInfraEnv(1)
		updates := map[string]interface{}{
			"user_name":    "other@test.example.com",
			"email_domain": "test.example.com",
			"org_id":       "456",
		}
		err = db.Model(&common.InfraEnv{}).Where("id = ?", id).Updates(updates).Error
		Expect(err).ToNot(HaveOccurred())

		err = migrateTo(db, "20210825135341")
		Expect(err).NotTo(HaveOccurred())

		var infraEnv common.InfraEnv
		err = db.First(&infraEnv, "id = ?", id).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(infraEnv.UserName).To(Equal("other@test.example.com"))
		Expect(infraEnv.EmailDomain).To(Equal("test.example.com"))
		Expect(infraEnv.OrgID).To(Equal("456"))
	})

	It("ignores infraenvs without cluster ids", func() {
		err := migrateToBefore(db, "20210825135341")
		Expect(err).ToNot(HaveOccurred())

		infraEnvID := strfmt.UUID(uuid.New().String())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:          infraEnvID,
			UserName:    "user@example.com",
			EmailDomain: "example.com",
			OrgID:       "111",
		}}).Error
		Expect(err).NotTo(HaveOccurred())

		err = migrateTo(db, "20210825135341")
		Expect(err).NotTo(HaveOccurred())

		var infraEnv common.InfraEnv
		err = db.First(&infraEnv, "id = ?", infraEnvID).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(infraEnv.UserName).To(Equal("user@example.com"))
		Expect(infraEnv.EmailDomain).To(Equal("example.com"))
		Expect(infraEnv.OrgID).To(Equal("111"))
	})
})
