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

var _ = Describe("changeClusterSshKeyToText", func() {
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
			ID:           &clusterID,
			SSHPublicKey: sshKey,
		}}
		err = db.Create(&cluster).Error
		Expect(err).NotTo(HaveOccurred())

		err = gm.MigrateTo("20220113152127")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(dbName, &common.Cluster{}, "ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))

		err = gm.RollbackMigration(changeClusterSshKeyToText())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("VARCHAR"))

		err = gm.MigrateTo("20220113152127")
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Cluster{}, "ssh_public_key")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("TEXT"))

		cluster := &common.Cluster{}
		Expect(db.First(cluster).Error).ShouldNot(HaveOccurred())
		Expect(cluster.SSHPublicKey).Should(Equal(sshKey))
	})
})
