package cluster

import (
	"context"
	"errors"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/stream"
	"gorm.io/gorm"
)

var newStatus = "newStatus"
var newStatusInfo = "newStatusInfo"

var _ = Describe("update_cluster_state", func() {
	var (
		ctx             = context.Background()
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
			cluster, err = UpdateCluster(ctx, common.GetTestLog(), db, nil, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(cluster.Status)).Should(Equal(newStatus))
			Expect(*cluster.StatusInfo).Should(Equal(newStatusInfo))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				_, err = UpdateCluster(ctx, common.GetTestLog(), db, nil, *cluster.ID, *cluster.Status, "1")
				Expect(err).Should(HaveOccurred())
				_, err = UpdateCluster(ctx, common.GetTestLog(), db, nil, *cluster.ID, *cluster.Status, "1", "2", "3")
				Expect(err).Should(HaveOccurred())
			})

			It("no_matching_rows", func() {
				_, err = UpdateCluster(ctx, common.GetTestLog(), db, nil, *cluster.ID, "otherStatus", "status", newStatus)
				Expect(err).Should(HaveOccurred())
			})

			AfterEach(func() {
				Expect(db.First(&cluster, "id = ?", cluster.ID).Error).ShouldNot(HaveOccurred())
				Expect(*cluster.Status).ShouldNot(Equal(newStatus))
				Expect(*cluster.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(cluster.StatusUpdatedAt.Equal(lastUpdatedTime)).Should(BeTrue())
			})
		})

		It("db_failure", func() {
			common.CloseDB(db)
			_, err = UpdateCluster(ctx, common.GetTestLog(), db, nil, *cluster.ID, *cluster.Status, "status", newStatus)
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
	It("1 hosts ready to install, 1 pending for input and 1 insufficient", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusPendingForInput, db)
		createHost(*cluster.ID, models.HostStatusInsufficient, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(one))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(three))
	})
	It("2 discovering and 1 disconnected", func() {
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusDisconnected, db)
		c := getClusterFromDB(*cluster.ID, db)
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(zero))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(three))
	})
	It("1 insufficient, 1 known and 1 discovering, with get cluster with hosts", func() {
		createHost(*cluster.ID, models.HostStatusKnown, db)
		createHost(*cluster.ID, models.HostStatusDiscovering, db)
		createHost(*cluster.ID, models.HostStatusInsufficient, db)
		c, err := common.GetClusterFromDBWithHosts(db, *cluster.ID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(c.Cluster.TotalHostCount).Should(Equal(three))
		Expect(c.Cluster.ReadyHostCount).Should(Equal(one))
		Expect(c.Cluster.EnabledHostCount).Should(Equal(three))
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

	It("cluster A with 3 hosts ready to install and cluster B with 2 ready and 1 insufficient", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusKnown, db)
		createHost(id2, models.HostStatusInsufficient, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(three))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(two))
		Expect(clusters[1].EnabledHostCount).To(Equal(three))
	})
	It("cluster A with 1 hosts ready to install, 1 disconnected and 1 discovering, and cluster B with 3 disconnected", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(one))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(zero))
		Expect(clusters[1].EnabledHostCount).To(Equal(three))
	})
	It("cluster A with 1 hosts ready to install, 1 disconnected and 1 discovering, and cluster B with 3 disconnected", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)

		clusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading, true, "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(clusters)).To(Equal(2))
		Expect(clusters[0].TotalHostCount).To(Equal(three))
		Expect(clusters[0].ReadyHostCount).To(Equal(one))
		Expect(clusters[0].EnabledHostCount).To(Equal(three))
		Expect(clusters[1].TotalHostCount).To(Equal(three))
		Expect(clusters[1].ReadyHostCount).To(Equal(zero))
		Expect(clusters[1].EnabledHostCount).To(Equal(three))
	})
	It("Get Clusters with skipped eager loading", func() {
		createHost(id1, models.HostStatusKnown, db)
		createHost(id1, models.HostStatusDisconnected, db)
		createHost(id1, models.HostStatusDiscovering, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)
		createHost(id2, models.HostStatusDisconnected, db)

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

var _ = Describe("UpdateMachineNetwork", func() {
	var (
		db *gorm.DB
	)

	tests := []struct {
		name                           string
		clusterMachineNetworks         []*models.MachineNetwork
		newMachineCidr                 string
		expectedClusterMachineNetworks []*models.MachineNetwork
		update                         bool
	}{
		{
			name:   "empty cluster with an empty new value",
			update: false,
		},
		{
			name:                           "empty cluster with non-empty new value",
			newMachineCidr:                 string(common.TestIPv4Networking.MachineNetworks[0].Cidr),
			expectedClusterMachineNetworks: common.TestIPv4Networking.MachineNetworks,
			update:                         true,
		},
		{
			name:                   "cluster with single machine network with an empty new value",
			clusterMachineNetworks: common.TestIPv4Networking.MachineNetworks,
			update:                 true,
		},
		{
			name:                           "cluster with single machine network with existing new value",
			clusterMachineNetworks:         common.TestIPv4Networking.MachineNetworks,
			newMachineCidr:                 string(common.TestIPv4Networking.MachineNetworks[0].Cidr),
			expectedClusterMachineNetworks: common.TestIPv4Networking.MachineNetworks,
			update:                         false,
		},
		{
			name:                           "cluster with single machine network with different new value",
			clusterMachineNetworks:         common.TestIPv4Networking.MachineNetworks,
			newMachineCidr:                 "5.6.7.0/24",
			expectedClusterMachineNetworks: []*models.MachineNetwork{{Cidr: "5.6.7.0/24"}},
			update:                         true,
		},
		{
			name:                           "cluster with multiple machine networks with an empty new value",
			clusterMachineNetworks:         append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			newMachineCidr:                 "",
			expectedClusterMachineNetworks: []*models.MachineNetwork{},
			update:                         true,
		},
		{
			// (MGMT-9915) This test scenario is an accepted collateral that in a full deployment
			//             will be prevented by another validation. It does not update the DB
			//             if the update request contains only single network that is equal to the
			//             first network already configured.
			//             This scenario reflects an use case when an old version of requester
			//             would try to interact with an object created by the new version and
			//             could potentially break it.
			name:                           "cluster with multiple machine networks with existing primary new value",
			clusterMachineNetworks:         append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			newMachineCidr:                 string(common.TestIPv4Networking.MachineNetworks[0].Cidr),
			expectedClusterMachineNetworks: append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			update:                         false,
		},
		{
			name:                           "cluster with multiple machine networks with existing secondary new value",
			clusterMachineNetworks:         append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			newMachineCidr:                 string(common.TestIPv6Networking.MachineNetworks[0].Cidr),
			expectedClusterMachineNetworks: common.TestIPv6Networking.MachineNetworks,
			update:                         true,
		},
		{
			name:                           "cluster with multiple machine networks with different new value",
			clusterMachineNetworks:         append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			newMachineCidr:                 "5.6.7.0/24",
			expectedClusterMachineNetworks: []*models.MachineNetwork{{Cidr: "5.6.7.0/24"}},
			update:                         true,
		},
	}

	BeforeEach(func() {
		db, _ = common.PrepareTestDB()

	})

	for i := range tests {
		test := tests[i]
		It(test.name, func() {
			id := strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:              &id,
					MachineNetworks: test.clusterMachineNetworks,
				},
				MachineNetworkCidrUpdatedAt: time.Now().Add(-2 * time.Minute),
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			cluster, err := common.GetClusterFromDB(common.LoadTableFromDB(db, common.MachineNetworksTable), id, common.SkipEagerLoading)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(UpdateMachineNetwork(db, cluster, []string{test.newMachineCidr})).ShouldNot(HaveOccurred())

			var clusterFromDb *common.Cluster
			clusterFromDb, err = common.GetClusterFromDB(common.LoadTableFromDB(db, common.MachineNetworksTable), id, common.SkipEagerLoading)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(clusterFromDb.MachineNetworks).To(HaveLen(len(test.expectedClusterMachineNetworks)))
			for idx := range clusterFromDb.MachineNetworks {
				Expect(clusterFromDb.MachineNetworks[idx].Cidr).To(Equal(test.expectedClusterMachineNetworks[idx].Cidr))
			}

			// TODO(MGMT-9751-remove-single-network)
			Expect(clusterFromDb.MachineNetworkCidr).To(Equal(""))

			if test.update {
				Expect(clusterFromDb.MachineNetworkCidrUpdatedAt).NotTo(Equal(cluster.MachineNetworkCidrUpdatedAt))
			} else {
				Expect(clusterFromDb.MachineNetworkCidrUpdatedAt).To(Equal(cluster.MachineNetworkCidrUpdatedAt))
			}
		})
	}
})

var _ = Describe("UpdateCluster and notify event stream", func() {
	var (
		ctx        = context.Background()
		dbName     string
		db         *gorm.DB
		cluster    *common.Cluster
		mockStream *stream.MockEventStreamWriter
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())

		db, dbName = common.PrepareTestDB()
		mockStream = stream.NewMockEventStreamWriter(ctrl)
		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     &common.TestDefaultConfig.Status,
			StatusInfo: &common.TestDefaultConfig.StatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	When("notifying with an empty stream", func() {
		It("should not send an event", func() {
			mockStream.EXPECT().Write(ctx, "ClusterState", []byte((*cluster).ID.String()), cluster).Times(0)
			notifyEventStream(ctx, nil, cluster, common.GetTestLog())
		})
	})

	When("notifying with a stream", func() {
		It("should send an event", func() {
			mockStream.EXPECT().Write(ctx, "ClusterState", []byte((*cluster).ID.String()), cluster).Times(1).Return(nil)
			notifyEventStream(ctx, mockStream, cluster, common.GetTestLog())
		})
	})

	When("cluster is successfully updated", func() {
		It("stream an event", func() {
			mockStream.EXPECT().Write(ctx, "ClusterState", []byte((*cluster).ID.String()), gomock.Any()).Times(1).Return(nil)
			var err error
			cluster, err = UpdateCluster(ctx, common.GetTestLog(), db, mockStream, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(cluster.Status)).Should(Equal(newStatus))
			Expect(*cluster.StatusInfo).Should(Equal(newStatusInfo))
		})
	})

	When("stream fails", func() {
		It("successfully updates cluster anyway", func() {
			mockStream.EXPECT().Write(ctx, "ClusterState", []byte((*cluster).ID.String()), gomock.Any()).Times(1).Return(errors.New("Stream failed"))
			_, err := UpdateCluster(ctx, common.GetTestLog(), db, mockStream, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	When("Wrong input is provided", func() {
		It("fails to update", func() {
			mockStream.EXPECT().Write(ctx, "ClusterState", []byte(cluster.ID.String()), gomock.Any()).Times(0)
			_, err := UpdateCluster(ctx, common.GetTestLog(), db, mockStream, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo, "this should fail")
			Expect(err).To(HaveOccurred())
		})
	})

	When("DB failure fails update", func() {
		It("does not stream events", func() {
			common.CloseDB(db)
			mockStream.EXPECT().Write(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			_, err := UpdateCluster(ctx, common.GetTestLog(), db, mockStream, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo, "this should fail")
			Expect(err).To(HaveOccurred())
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})
