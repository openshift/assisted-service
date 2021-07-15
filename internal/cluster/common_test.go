package cluster

import (
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var newStatus = "newStatus"
var newStatusInfo = "newStatusInfo"

var _ = Describe("update_cluster_state", func() {
	var (
		db              *gorm.DB
		cluster         *common.Cluster
		lastUpdatedTime strfmt.DateTime
		err             error
		dbName          string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     &common.TestDefaultConfig.Status,
			StatusInfo: &common.TestDefaultConfig.StatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		lastUpdatedTime = cluster.StatusUpdatedAt
	})

	Describe("UpdateCluster", func() {
		It("change_status", func() {
			cluster, err = UpdateCluster(common.GetTestLog(), db, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(cluster.Status)).Should(Equal(newStatus))
			Expect(*cluster.StatusInfo).Should(Equal(newStatusInfo))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				_, err = UpdateCluster(common.GetTestLog(), db, *cluster.ID, *cluster.Status, "1")
				Expect(err).Should(HaveOccurred())
				_, err = UpdateCluster(common.GetTestLog(), db, *cluster.ID, *cluster.Status, "1", "2", "3")
				Expect(err).Should(HaveOccurred())
			})

			It("no_matching_rows", func() {
				_, err = UpdateCluster(common.GetTestLog(), db, *cluster.ID, "otherStatus", "status", newStatus)
				Expect(err).Should(HaveOccurred())
			})

			AfterEach(func() {
				Expect(db.First(&cluster, "id = ?", cluster.ID).Error).ShouldNot(HaveOccurred())
				Expect(*cluster.Status).ShouldNot(Equal(newStatus))
				Expect(*cluster.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(cluster.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
			})
		})

		It("db_failure", func() {
			db.Close()
			_, err = UpdateCluster(common.GetTestLog(), db, *cluster.ID, *cluster.Status, "status", newStatus)
			Expect(err).Should(HaveOccurred())
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})

const (
	zero int64 = iota
	one
	two
	three
)

var _ = Describe("host count with 1 cluster", func() {
	var (
		db      *gorm.DB
		cluster *common.Cluster
		dbName  string
	)

	createHost := func(clusterId strfmt.UUID, state string, db *gorm.DB) {
		hostId := strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID:         &hostId,
			InfraEnvID: clusterId,
			ClusterID:  clusterId,
			Role:       models.HostRoleMaster,
			Status:     swag.String(state),
			Inventory:  common.GenerateTestDefaultInventory(),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     &common.TestDefaultConfig.Status,
			StatusInfo: &common.TestDefaultConfig.StatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	It("3 hosts ready to install", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusKnown, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(three))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(three))
	})
	It("2 hosts ready to install and 1 insufficient", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusInsufficient, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(two))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(three))
	})
	It("2 hosts ready to install and 1 disabled", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(two))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(two))
	})
	It("1 hosts ready to install, 1 pending for input and 1 disabled", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusPendingForInput, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(one))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(two))
	})
	It("2 discovering and 1 disabled", func() {
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(zero))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(two))
	})
	It("1 disconnected,  and 2 disabled", func() {
		createHost(*cluster.ID, models.HostStatusDisconnected, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(zero))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(one))
	})
	It("1 disabled, 1 known and 1 discovering, with get cluster without disabled hosts", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusDisabled, db)
		c, err := common.GetClusterFromDBWithoutDisabledHosts(db, *cluster.ID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(c.Cluster.TotalHostCount).Should(Equal(two))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(one))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(two))
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})

var _ = Describe("host count with 2 cluster", func() {
	var (
		db     *gorm.DB
		id1    = strfmt.UUID(uuid.New().String())
		id2    = strfmt.UUID(uuid.New().String())
		dbName string
	)

	createHost := func(clusterId strfmt.UUID, state string, db *gorm.DB) {
		hostId := strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID:         &hostId,
			ClusterID:  clusterId,
			InfraEnvID: clusterId,
			Role:       models.HostRoleMaster,
			Status:     swag.String(state),
			Inventory:  common.GenerateTestDefaultInventory(),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cluster := &common.Cluster{Cluster: models.Cluster{
			ID:         &id1,
			Status:     &common.TestDefaultConfig.Status,
			StatusInfo: &common.TestDefaultConfig.StatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:         &id2,
			Status:     &common.TestDefaultConfig.Status,
			StatusInfo: &common.TestDefaultConfig.StatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	It("cluster A with 3 hosts ready to install and cluster B with 2 ready and 1 disabled", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusDisabled, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(three))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(two))
		Expect(clusters[1].EnabledHostCount).To(Equal(two))
	})
	It("cluster A with 1 hosts ready to install, 1 disconnected and 1 discovering, and cluster B with 3 disabled", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(one))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(zero))
		Expect(clusters[1].EnabledHostCount).To(Equal(zero))
	})
	It("cluster A with 1 hosts ready to install, 1 disconnected and 1 discovering, and cluster B with 3 disabled", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(one))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(zero))
		Expect(clusters[1].EnabledHostCount).To(Equal(zero))
	})
	It("Get Clusters with skipped eager loading", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)
		createHost(id2, models.HostStatusDisabled, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.SkipEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(zero))
		Expect(clusters[0].ReadyHostCount).To(Equal(zero))
		Expect(clusters[0].EnabledHostCount).To(Equal(zero))
		Expect(clusters[1].TotalHostCount).To(Equal(zero))
		Expect(clusters[1].ReadyHostCount).To(Equal(zero))
		Expect(clusters[1].EnabledHostCount).To(Equal(zero))
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})
