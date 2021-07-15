package cluster

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("installer", func() {
	var (
		ctx              = context.Background()
		installerManager InstallationAPI
		db               *gorm.DB
		id               strfmt.UUID
		cluster          common.Cluster
		hostsIds         []strfmt.UUID
		dbName           string
		eventsHandler    events.Handler
	)

	BeforeEach(func() {
		eventsHandler = events.New(db, logrus.New())
		db, dbName = common.PrepareTestDB()
		installerManager = NewInstaller(common.GetTestLog(), db, eventsHandler)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusReady),
		}}

		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	Context("get master nodes ids", func() {
		It("test getting master ids", func() {

			for i := 0; i < 3; i++ {
				hostsIds = append(hostsIds, addHost(models.HostRoleMaster, models.HostStatusKnown, id, db))
			}
			masterKnownIds := hostsIds
			hostsIds = append(hostsIds, addHost(models.HostRoleWorker, models.HostStatusKnown, id, db))
			hostsIds = append(hostsIds, addHost(models.HostRoleMaster, models.HostStatusDiscovering, id, db))

			replyMasterNodesIds, err := installerManager.GetMasterNodesIds(ctx, &cluster, db)
			Expect(err).Should(BeNil())
			Expect(len(masterKnownIds)).Should(Equal(len(replyMasterNodesIds)))
			for _, iid := range masterKnownIds {
				Expect(checkIfIdInArr(iid, replyMasterNodesIds))
			}
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

func addHost(role models.HostRole, state string, clusterId strfmt.UUID, db *gorm.DB) strfmt.UUID {

	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: clusterId,
		ClusterID:  clusterId,
		Status:     swag.String(state),
		Role:       role,
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	return hostId
}

func checkIfIdInArr(a strfmt.UUID, list []*strfmt.UUID) bool {
	for _, b := range list {
		if b == &a {
			return true
		}
	}
	return false
}
