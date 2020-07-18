package cluster

import (
	context "context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ready_state", func() {
	var (
		ctx     = context.Background()
		state   API
		db      *gorm.DB
		id      strfmt.UUID
		cluster common.Cluster
		dbName  = "cluster_ready_state"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		state = &Manager{log: getTestLog(), ready: NewReadyState(getTestLog(), db)}

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusReady),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		addInstallationRequirements(id, db)

		cluster = geCluster(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusReady))
		Expect(len(cluster.Hosts)).Should(Equal(3))
	})

	Context("refresh_state", func() {
		It("cluster is satisfying the install requirements", func() {
			clusterAfterRefresh, updateErr := state.RefreshStatus(ctx, &cluster, db)

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(clusterStatusReady))
		})

		It("cluster is not satisfying the install requirements", func() {
			Expect(db.Where("cluster_id = ?", cluster.ID).Delete(&models.Host{}).Error).NotTo(HaveOccurred())

			cluster = geCluster(*cluster.ID, db)
			clusterAfterRefresh, updateErr := state.RefreshStatus(ctx, &cluster, db)

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(clusterStatusInsufficient))
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})
