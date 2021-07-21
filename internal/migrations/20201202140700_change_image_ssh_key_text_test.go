package migrations

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	gormigrate "gopkg.in/gormigrate.v1"
)

var _ = Describe("changeOverridesToText", func() {
	var (
		db        *gorm.DB
		dbName    string
		gm        *gormigrate.Gormigrate
		clusterID strfmt.UUID
		err       error
		sshKey    = "some ssh key"
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())

		// create cluster in order to get rows from DB
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
			ImageInfo: &models.ImageInfo{
				SSHPublicKey: sshKey,
			},
		}}
		err = db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		err = gm.MigrateTo("20201202140700")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(db, &common.Cluster{}, "image_ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))

		err = gm.RollbackMigration(changeImageSSHKeyToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &common.Cluster{}, "image_ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("VARCHAR"))

		err = gm.MigrateTo("20201202140700")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(db, &common.Cluster{}, "image_ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(t).To(Equal("TEXT"))

		cluster := &common.Cluster{}
		Expect(db.First(cluster).Error).ShouldNot(HaveOccurred())
		Expect(cluster.ImageInfo.SSHPublicKey).Should(Equal(sshKey))
	})
})
