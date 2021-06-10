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
	"github.com/pkg/errors"
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

	Context("install cluster", func() {
		It("cluster is insufficient", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusInsufficient, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is expected to have exactly 3 known master to be installed, got 0", cluster.ID).Error()))
		})
		It("cluster is installing", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusInstalling, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is already installing", cluster.ID).Error()))
		})
		It("cluster is finalizing", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusFinalizing, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is already %s", cluster.ID, models.ClusterStatusFinalizing).Error()))
		})
		It("cluster is in error", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusError, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s has a error", cluster.ID).Error()))
		})
		It("cluster is installed", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusInstalled, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is already installed", cluster.ID).Error()))
		})
		It("cluster is in unknown state", func() {
			cluster = updateClusterState(cluster, "its fun to be unknown", db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s state is unclear - cluster state: its fun to be unknown", cluster.ID).Error()))
		})
		It("cluster is ready", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusReady, db)
			Expect(installerManager.Install(ctx, &cluster, db)).Should(HaveOccurred())
		})
		It("cluster is ready", func() {
			cluster = updateClusterState(cluster, models.ClusterStatusPreparingForInstallation, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err).Should(BeNil())

			cluster = getClusterFromDB(*cluster.ID, db)
			Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInstalling))
		})
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

func updateClusterState(cluster common.Cluster, state string, db *gorm.DB) common.Cluster {
	cluster.Status = swag.String(state)
	Expect(db.Model(&cluster).Update("status", state).Error).NotTo(HaveOccurred())
	return cluster
}

func addHost(role models.HostRole, state string, clusterId strfmt.UUID, db *gorm.DB) strfmt.UUID {

	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:        &hostId,
		ClusterID: clusterId,
		Status:    swag.String(state),
		Role:      role,
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
