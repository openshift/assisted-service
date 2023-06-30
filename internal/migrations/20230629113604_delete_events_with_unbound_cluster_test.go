package migrations

import (
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("deleteEventsWithUnboundCluster", func() {
	var (
		db           *gorm.DB
		dbName       string
		clusterID    = strfmt.UUID("46a8d745-dfce-4fd8-9df0-549ee8eabb3d")
		nonClusterID = strfmt.UUID("44444444-dfce-4fd8-9df0-549ee8eabb3d")
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterID, OpenshiftClusterID: strfmt.UUID("134ce3ea-512e-4a11-bc4c-aeac1bdf3820"), UserName: "user1", OrgID: "org1"}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		message := "this is my event"
		event := common.Event{Event: models.Event{ClusterID: &clusterID, Message: &message}}
		Expect(db.Create(&event).Error).ShouldNot(HaveOccurred())
		event = common.Event{Event: models.Event{ClusterID: &nonClusterID, Message: &message}}
		Expect(db.Create(&event).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		numRawEvents := func(clusterID *strfmt.UUID) int {
			var count int64
			db.Model(&models.Event{}).Where("cluster_id = ?", clusterID).Count(&count)
			return int(count)
		}

		err := migrateToBefore(db, "20230629113604")
		Expect(err).ToNot(HaveOccurred())

		Expect(numRawEvents(&clusterID)).To(Equal(1))
		Expect(numRawEvents(&nonClusterID)).To(Equal(1))

		err = migrateTo(db, "20230629113604")
		Expect(err).NotTo(HaveOccurred())
		Expect(numRawEvents(&clusterID)).To(Equal(1))
		Expect(numRawEvents(&nonClusterID)).To(Equal(0))
	})
})
