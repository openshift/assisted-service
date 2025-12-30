package common

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("GetHostCountByRole", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID = strfmt.UUID(uuid.New().String())
		HostID1   = strfmt.UUID(uuid.New().String())
		HostID2   = strfmt.UUID(uuid.New().String())
		HostID3   = strfmt.UUID(uuid.New().String())
		HostID4   = strfmt.UUID(uuid.New().String())
		HostID5   = strfmt.UUID(uuid.New().String())
		HostID6   = strfmt.UUID(uuid.New().String())
		HostID7   = strfmt.UUID(uuid.New().String())
		HostID8   = strfmt.UUID(uuid.New().String())
	)

	BeforeEach(func() {
		db, dbName = PrepareTestDB()
	})

	AfterEach(func() {
		DeleteTestDB(db, dbName)
	})

	Context("should succeed", func() {
		Context("with suggested role", func() {
			It("and some records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				masterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleMaster, true)
				Expect(err).ToNot(HaveOccurred())

				arbiterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleArbiter, true)
				Expect(err).ToNot(HaveOccurred())

				workerCount, err := GetHostCountByRole(db, clusterID, models.HostRoleWorker, true)
				Expect(err).ToNot(HaveOccurred())

				autoAssignCount, err := GetHostCountByRole(db, clusterID, models.HostRoleAutoAssign, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(*masterCount).To(BeEquivalentTo(3))
				Expect(*arbiterCount).To(BeEquivalentTo(0))
				Expect(*workerCount).To(BeEquivalentTo(3))
				Expect(*autoAssignCount).To(BeEquivalentTo(2))
			})

			It("no records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				bootstrapHostCount, err := GetHostCountByRole(db, clusterID, models.HostRoleBootstrap, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(*bootstrapHostCount).To(BeEquivalentTo(0))
			})
		})

		Context("with non-suggested role", func() {
			It("and some records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				masterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleMaster, false)
				Expect(err).ToNot(HaveOccurred())

				arbiterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleArbiter, false)
				Expect(err).ToNot(HaveOccurred())

				workerCount, err := GetHostCountByRole(db, clusterID, models.HostRoleWorker, false)
				Expect(err).ToNot(HaveOccurred())

				autoAssignCount, err := GetHostCountByRole(db, clusterID, models.HostRoleAutoAssign, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(*masterCount).To(BeEquivalentTo(1))
				Expect(*arbiterCount).To(BeEquivalentTo(0))
				Expect(*workerCount).To(BeEquivalentTo(1))
				Expect(*autoAssignCount).To(BeEquivalentTo(6))
			})

			It("no records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				bootstrapHostCount, err := GetHostCountByRole(db, clusterID, models.HostRoleBootstrap, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(*bootstrapHostCount).To(BeEquivalentTo(0))
			})
		})
	})
})

var _ = Describe("GetClusterFromDB", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID = strfmt.UUID(uuid.New().String())
		primaryv4 = PrimaryIPStackV4
		primaryv6 = PrimaryIPStackV6
	)

	BeforeEach(func() {
		db, dbName = PrepareTestDB()
	})

	AfterEach(func() {
		DeleteTestDB(db, dbName)
	})

	Context("Network order", func() {
		Context("single stack clusters", func() {
			It("should successfully return a single stack ipv4 cluster", func() {
				cluster := ipv4Cluster(clusterID)
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks).To(Equal(cluster.MachineNetworks))
			})
			It("should successfully return a single stack ipv6 cluster", func() {
				cluster := ipv6Cluster(clusterID)
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks).To(Equal(cluster.MachineNetworks))
			})
		})
		Context("dual stack clusters", func() {
			It("should successfully return a dual-stack cluster where the ipv4 networks are first with primary_ip_stack set to ipv4", func() {
				cluster := dualStackCluster(clusterID)
				cluster.PrimaryIPStack = &primaryv4
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks).To(Equal(cluster.MachineNetworks))
				Expect(clusterResult.APIVips).To(Equal(cluster.APIVips))
				Expect(clusterResult.IngressVips).To(Equal(cluster.IngressVips))
				Expect(clusterResult.ServiceNetworks).To(Equal(cluster.ServiceNetworks))
				Expect(clusterResult.ClusterNetworks).To(Equal(cluster.ClusterNetworks))
			})
			It("should successfully return a dual-stack cluster where the ipv6 networks are first with primary_ip_stack set to ipv6", func() {
				cluster := dualStackCluster(clusterID)
				cluster.PrimaryIPStack = &primaryv6
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks[0].Cidr).To(Equal(cluster.MachineNetworks[1].Cidr))
				Expect(clusterResult.MachineNetworks[1].Cidr).To(Equal(cluster.MachineNetworks[0].Cidr))
				Expect(clusterResult.APIVips[0].IP).To(Equal(cluster.APIVips[1].IP))
				Expect(clusterResult.APIVips[1].IP).To(Equal(cluster.APIVips[0].IP))
				Expect(clusterResult.IngressVips[0].IP).To(Equal(cluster.IngressVips[1].IP))
				Expect(clusterResult.IngressVips[1].IP).To(Equal(cluster.IngressVips[0].IP))
				Expect(clusterResult.ServiceNetworks[0].Cidr).To(Equal(cluster.ServiceNetworks[1].Cidr))
				Expect(clusterResult.ServiceNetworks[1].Cidr).To(Equal(cluster.ServiceNetworks[0].Cidr))
				Expect(clusterResult.ClusterNetworks[0].Cidr).To(Equal(cluster.ClusterNetworks[1].Cidr))
				Expect(clusterResult.ClusterNetworks[1].Cidr).To(Equal(cluster.ClusterNetworks[0].Cidr))
			})
			It("should successfully return a dual-stack cluster with primary_ip_stack not set and default to ipv4 networks first", func() {
				cluster := dualStackCluster(clusterID)
				cluster.PrimaryIPStack = nil
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks).To(Equal(cluster.MachineNetworks))
				Expect(clusterResult.APIVips).To(Equal(cluster.APIVips))
				Expect(clusterResult.IngressVips).To(Equal(cluster.IngressVips))
				Expect(clusterResult.ServiceNetworks).To(Equal(cluster.ServiceNetworks))
				Expect(clusterResult.ClusterNetworks).To(Equal(cluster.ClusterNetworks))
			})
			It("should successfully return all IPs ordered by primary_ip_stack when the cluster is created with the primary_ip_stack set to ipv4 and the networks are not in the correct order", func() {
				cluster := dualStackCluster(clusterID)
				cluster.PrimaryIPStack = &primaryv4
				By("switching the API VIPs to be IPv6 first")
				cluster.APIVips[0].IP = "2001:db8::1"
				cluster.APIVips[1].IP = "10.0.1.1"
				By("creating the cluster")
				err := db.Create(cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(clusterResult.APIVips[0].IP)).To(Equal("10.0.1.1"))
				Expect(string(clusterResult.APIVips[1].IP)).To(Equal("2001:db8::1"))
				Expect(clusterResult.IngressVips).To(Equal(cluster.IngressVips))
				Expect(clusterResult.ServiceNetworks).To(Equal(cluster.ServiceNetworks))
				Expect(clusterResult.ClusterNetworks).To(Equal(cluster.ClusterNetworks))
				Expect(clusterResult.MachineNetworks).To(Equal(cluster.MachineNetworks))
			})
		})
		Context("empty networks", func() {
			It("should successfully return a cluster without networks set", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
					},
				}
				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				clusterResult, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(clusterResult.MachineNetworks).To(BeEmpty())
				Expect(clusterResult.APIVips).To(BeEmpty())
				Expect(clusterResult.IngressVips).To(BeEmpty())
				Expect(clusterResult.ServiceNetworks).To(BeEmpty())
				Expect(clusterResult.ClusterNetworks).To(BeEmpty())
			})
		})
	})
})

func dualStackCluster(clusterID strfmt.UUID) *Cluster {
	return &Cluster{
		Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{
				{Cidr: "10.0.0.0/16"},
				{Cidr: "2001:db8::/64"},
			},
			APIVips: []*models.APIVip{
				{IP: "10.0.1.1"},
				{IP: "2001:db8::1"},
			},
			IngressVips: []*models.IngressVip{
				{IP: "10.0.1.2"},
				{IP: "2001:db8::2"},
			},
			ServiceNetworks: []*models.ServiceNetwork{
				{Cidr: "172.30.0.0/16"},
				{Cidr: "2001:db8:1::/64"},
			},
			ClusterNetworks: []*models.ClusterNetwork{
				{Cidr: "10.128.0.0/14"},
				{Cidr: "2001:db8:2::/64"},
			},
		},
	}
}
func ipv4Cluster(clusterID strfmt.UUID) *Cluster {
	return &Cluster{
		Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{
				{Cidr: "10.0.0.0/16"},
			},
			APIVips: []*models.APIVip{
				{IP: "10.0.1.1"},
			},
			IngressVips: []*models.IngressVip{
				{IP: "10.0.1.2"},
			},
			ServiceNetworks: []*models.ServiceNetwork{
				{Cidr: "172.30.0.0/16"},
			},
			ClusterNetworks: []*models.ClusterNetwork{
				{Cidr: "10.128.0.0/14"},
			},
		},
	}
}
func ipv6Cluster(clusterID strfmt.UUID) *Cluster {
	return &Cluster{
		Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{
				{Cidr: "2001:db8::/64"},
			},
			APIVips: []*models.APIVip{
				{IP: "2001:db8::1"},
			},
			IngressVips: []*models.IngressVip{
				{IP: "2001:db8::2"},
			},
			ServiceNetworks: []*models.ServiceNetwork{
				{Cidr: "2001:db8:1::/64"},
			},
			ClusterNetworks: []*models.ClusterNetwork{
				{Cidr: "2001:db8:2::/64"},
			},
		},
	}
}
