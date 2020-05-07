package cluster

import (
	"context"

	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("installer", func() {
	var (
		ctx              = context.Background()
		installerManager InstallationAPI
		db               *gorm.DB
		id               strfmt.UUID
		cluster          models.Cluster
		hostsIds         []strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		installerManager = NewInstaller(getTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		cluster = models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusReady),
		}

		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	Context("install cluster", func() {
		It("cluster is insufficient", func() {
			cluster = updateClusterState(cluster, clusterStatusInsufficient, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is missing the resources to be installed", cluster.ID).Error()))
		})
		It("cluster is installing", func() {
			cluster = updateClusterState(cluster, clusterStatusInstalling, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is already installing", cluster.ID).Error()))
		})
		It("cluster is in error", func() {
			cluster = updateClusterState(cluster, clusterStatusError, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s has a error", cluster.ID).Error()))
		})
		It("cluster is installed", func() {
			cluster = updateClusterState(cluster, clusterStatusInstalled, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s is already installed", cluster.ID).Error()))
		})
		It("cluster is in unknown state", func() {
			cluster = updateClusterState(cluster, "its fun to be unknown", db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err.Error()).Should(MatchRegexp(errors.Errorf("cluster %s state is unclear - cluster state: its fun to be unknown", cluster.ID).Error()))
		})
		It("cluster is ready", func() {
			cluster = updateClusterState(cluster, clusterStatusReady, db)
			err := installerManager.Install(ctx, &cluster, db)
			Expect(err).Should(BeNil())

			Expect(db.Preload("Hosts").First(&cluster, "id = ?", cluster.ID).Error).ShouldNot(HaveOccurred())

			Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusInstalling))
		})
	})

	Context("get master nodes ids", func() {
		It("test getting master ids", func() {

			for i := 0; i < 3; i++ {
				hostsIds = append(hostsIds, addHost("master", host.HostStatusKnown, id, db))
			}
			masterKnownIds := hostsIds
			hostsIds = append(hostsIds, addHost("worker", host.HostStatusKnown, id, db))
			hostsIds = append(hostsIds, addHost("master", host.HostStatusDiscovering, id, db))

			replyMasterNodesIds, err := installerManager.GetMasterNodesIds(ctx, &cluster, db)
			Expect(err).Should(BeNil())
			Expect(len(masterKnownIds)).Should(Equal(len(replyMasterNodesIds)))
			for _, iid := range masterKnownIds {
				Expect(checkIfIdInArr(iid, replyMasterNodesIds))
			}
		})
	})
	AfterEach(func() {
		db.Close()
	})
})

func updateClusterState(cluster models.Cluster, state string, db *gorm.DB) models.Cluster {
	cluster.Status = swag.String(state)
	Expect(db.Model(&cluster).Update("status", state).Error).NotTo(HaveOccurred())
	return cluster
}

func addHost(role string, state string, clusterId strfmt.UUID, db *gorm.DB) strfmt.UUID {

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
