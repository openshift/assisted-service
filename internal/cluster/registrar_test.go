package cluster

import (
	context "context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("insufficient_state", func() {
	var (
		ctx          = context.Background()
		state        API
		db           *gorm.DB
		currentState = clusterStatusInsufficient
		id           strfmt.UUID
		updateReply  *UpdateReply
		updateErr    error
		cluster      models.Cluster
		host         models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		state = &Manager{insufficient: NewInsufficientState(getTestLog(), db)}
		registerManager := NewRegistrar(getTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		cluster = models.Cluster{
			Base: models.Base{
				ID: &id,
			},
			Status: swag.String(currentState),
		}

		updateReply, updateErr = registerManager.RegisterCluster(ctx, &cluster)
		Expect(updateErr).Should(BeNil())
		Expect(updateReply.State).Should(Equal(clusterStatusInsufficient))
		c := geCluster(*cluster.ID, db)
		Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInsufficient))
	})

	Context("refresh_state", func() {
		It("not answering requirement to be ready", func() {
			updateReply, updateErr = state.RefreshStatus(ctx, &cluster, db)
			Expect(updateErr).Should(BeNil())
			Expect(updateReply.State).Should(Equal(clusterStatusInsufficient))
			c := geCluster(*cluster.ID, db)
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInsufficient))
		})

		It("answering requirement to be ready", func() {
			addInstallationRequirements(id, db)
			updateReply, updateErr = state.RefreshStatus(ctx, &cluster, db)
			Expect(updateErr).Should(BeNil())
			Expect(updateReply.State).Should(Equal(clusterStatusReady))
			c := geCluster(*cluster.ID, db)
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusReady))

		})
	})

	Context("deregister", func() {
		It("unregister a registered cluster", func() {
			Expect(db.First(&host, "cluster_id = ?", cluster.ID).Error).Should(HaveOccurred())
			updateReply, updateErr = state.DeregisterCluster(ctx, &cluster)
			Expect(updateErr).Should(BeNil())
			Expect(updateReply.State).Should(Equal("unregistered"))
			Expect(db.First(&cluster, "id = ?", cluster.ID).Error).Should(HaveOccurred())
			Expect(db.First(&host, "cluster_id = ?", cluster.ID).Error).Should(HaveOccurred())

		})

		It("unregister a unregistered cluster", func() {
			unregisteredClusterId := strfmt.UUID(uuid.New().String())
			cluster.ID = &unregisteredClusterId
			updateReply, updateErr = state.DeregisterCluster(ctx, &cluster)
			Expect(updateReply.State).Should(Equal("unregistered"))
		})
	})

	AfterEach(func() {

		db.Close()
		updateReply = nil
		updateErr = nil
	})
})
