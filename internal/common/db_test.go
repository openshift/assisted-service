package common

import (
	"encoding/json"

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

		Context("Bootstrap network order", func() {
			addHost := func(bootstrap bool, ipv4, ipv6 []string) {
				hostID := strfmt.UUID(uuid.New().String())
				inv := models.Inventory{
					Interfaces: []*models.Interface{{
						Name:          "eth0",
						IPV4Addresses: ipv4,
						IPV6Addresses: ipv6,
					}},
				}
				invJSON, err := json.Marshal(&inv)
				Expect(err).ToNot(HaveOccurred())
				host := &Host{Host: models.Host{
					ID:         &hostID,
					InfraEnvID: clusterID,
					ClusterID:  &clusterID,
					Bootstrap:  bootstrap,
					Inventory:  string(invJSON),
				}}
				Expect(db.Create(host).Error).ToNot(HaveOccurred())
			}

			// Standalone cases: no networks or no IPs
			It("should handle no machine networks", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: []*models.MachineNetwork{},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, []string{"10.0.0.5/24"}, nil)

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(BeEmpty())
			})

			It("should handle no bootstrap host", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("should handle bootstrap host with no IPs", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, nil, nil)

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("should ignore non-bootstrap hosts", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(false, nil, []string{"2001:db8::5/64"})

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			// Single network: bootstrap ordering is a no-op
			It("single IPv4 network with IPv4 bootstrap", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, []string{"192.168.1.5/24"}, nil)

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(1))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
			})

			It("single IPv6 network with IPv6 bootstrap", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, nil, []string{"2001:db8::5/64"})

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(1))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
			})

			// Dual-stack: bootstrap determines order when no primary stack is set
			It("dual-stack with IPv6-only bootstrap should put IPv6 first", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, nil, []string{"2001:db8::5/64"})

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("192.168.1.0/24"))
			})

			It("dual-stack with IPv4-only bootstrap should put IPv4 first", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "2001:db8::/64"},
						{Cidr: "192.168.1.0/24"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, []string{"192.168.1.5/24"}, nil)

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("dual-stack with dual-stack bootstrap should match both networks", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, []string{"192.168.1.5/24"}, []string{"2001:db8::5/64"})

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("dual-stack with unmatched bootstrap IP should not reorder", func() {
				cluster := &Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "192.168.1.0/24"},
						{Cidr: "2001:db8::/64"},
					},
				}}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, []string{"172.16.0.5/24"}, nil)

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			// Primary IP stack takes precedence over bootstrap ordering
			It("should prefer IP family over bootstrap network in dual-stack", func() {
				cluster := &Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "2001:db8::/64"},
							{Cidr: "192.168.1.0/24"},
						},
					},
					PrimaryIPStack: &primaryv4,
				}
				Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
				addHost(true, nil, []string{"2001:db8::5/64"})

				result, err := GetClusterFromDB(db, clusterID, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.MachineNetworks).To(HaveLen(2))
				Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("192.168.1.0/24"))
				Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
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

var _ = Describe("DeleteSoftDeletedHost", func() {
	var (
		db         *gorm.DB
		dbName     string
		infraEnvID = strfmt.UUID(uuid.New().String())
		hostID     = strfmt.UUID(uuid.New().String())
	)

	BeforeEach(func() {
		db, dbName = PrepareTestDB()

		// Create infraEnv
		infraEnv := &InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &infraEnvID,
			},
		}
		Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		DeleteTestDB(db, dbName)
	})

	It("should successfully delete a soft-deleted host", func() {
		// Create and soft-delete a host
		host := &Host{
			Host: models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
			},
		}
		Expect(db.Create(host).Error).ToNot(HaveOccurred())
		Expect(db.Delete(host).Error).ToNot(HaveOccurred())

		// Verify host is soft-deleted (not found in normal query)
		var count int64
		Expect(db.Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))

		// Verify soft-deleted host exists with Unscoped
		Expect(db.Unscoped().Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))

		// Delete soft-deleted host
		err := DeleteSoftDeletedHost(db, hostID.String(), infraEnvID.String())
		Expect(err).ToNot(HaveOccurred())

		// Verify host is completely removed
		Expect(db.Unscoped().Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("should not delete an active (non-soft-deleted) host", func() {
		// Create active host
		host := &Host{
			Host: models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
			},
		}
		Expect(db.Create(host).Error).ToNot(HaveOccurred())

		// Attempt to delete soft-deleted host (should be no-op)
		err := DeleteSoftDeletedHost(db, hostID.String(), infraEnvID.String())
		Expect(err).ToNot(HaveOccurred())

		// Verify active host still exists
		var count int64
		Expect(db.Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})

	It("should succeed when no host exists at all", func() {
		// Call on non-existent host
		err := DeleteSoftDeletedHost(db, hostID.String(), infraEnvID.String())
		Expect(err).ToNot(HaveOccurred())

		// Verify no host exists
		var count int64
		Expect(db.Unscoped().Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))
	})

	It("should only delete host with matching composite key", func() {
		otherInfraEnvID := strfmt.UUID(uuid.New().String())

		// Create another infraEnv
		otherInfraEnv := &InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &otherInfraEnvID,
			},
		}
		Expect(db.Create(otherInfraEnv).Error).ToNot(HaveOccurred())

		// Create and soft-delete host in first infraEnv
		host1 := &Host{
			Host: models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
			},
		}
		Expect(db.Create(host1).Error).ToNot(HaveOccurred())
		Expect(db.Delete(host1).Error).ToNot(HaveOccurred())

		// Create and soft-delete host with same ID in different infraEnv
		host2 := &Host{
			Host: models.Host{
				ID:         &hostID,
				InfraEnvID: otherInfraEnvID,
			},
		}
		Expect(db.Create(host2).Error).ToNot(HaveOccurred())
		Expect(db.Delete(host2).Error).ToNot(HaveOccurred())

		// Delete soft-deleted host in first infraEnv only
		err := DeleteSoftDeletedHost(db, hostID.String(), infraEnvID.String())
		Expect(err).ToNot(HaveOccurred())

		// Verify first host is deleted
		var count int64
		Expect(db.Unscoped().Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), infraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(0)))

		// Verify second host still exists (soft-deleted)
		Expect(db.Unscoped().Model(&Host{}).Where("id = ? AND infra_env_id = ?", hostID.String(), otherInfraEnvID.String()).Count(&count).Error).ToNot(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})
})
