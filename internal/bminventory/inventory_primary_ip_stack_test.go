package bminventory

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"gorm.io/gorm"
)

var _ = Describe("Primary IP Stack Functionality", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		dbName    string
		ctx       = context.Background()
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		cfg = Config{
			DefaultClusterNetworkCidr:       "10.128.0.0/14",
			DefaultServiceNetworkCidr:       "172.30.0.0/16",
			DefaultClusterNetworkHostPrefix: 23,
		}

		bm = createInventory(db, cfg)

		clusterID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Describe("setPrimaryIPStack", func() {
		Context("Single stack clusters", func() {
			It("should set PrimaryIPStack to nil for IPv4-only cluster", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              &clusterID,
						MachineNetworks: []*models.MachineNetwork{{Cidr: "10.0.0.0/16"}},
						APIVips:         []*models.APIVip{{IP: "10.0.1.1"}},
						IngressVips:     []*models.IngressVip{{IP: "10.0.1.2"}},
						ServiceNetworks: []*models.ServiceNetwork{{Cidr: "172.30.0.0/16"}},
						ClusterNetworks: []*models.ClusterNetwork{{Cidr: "10.128.0.0/14"}},
					},
				}

				err := bm.setPrimaryIPStack(cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.PrimaryIPStack).To(BeNil())
			})

			It("should set PrimaryIPStack to nil for IPv6-only cluster", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              &clusterID,
						MachineNetworks: []*models.MachineNetwork{{Cidr: "2001:db8::/64"}},
						APIVips:         []*models.APIVip{{IP: "2001:db8::1"}},
						IngressVips:     []*models.IngressVip{{IP: "2001:db8::2"}},
						ServiceNetworks: []*models.ServiceNetwork{{Cidr: "2001:db8:1::/64"}},
						ClusterNetworks: []*models.ClusterNetwork{{Cidr: "2001:db8:2::/64"}},
					},
				}

				err := bm.setPrimaryIPStack(cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.PrimaryIPStack).To(BeNil())
			})
		})

		Context("Dual stack clusters", func() {
			It("should set PrimaryIPStack to IPv4 for IPv4-first dual stack", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:               &clusterID,
						OpenshiftVersion: "4.12.0", // Required for dual-stack support
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

				err := bm.setPrimaryIPStack(cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.PrimaryIPStack).ToNot(BeNil())
				Expect(*cluster.PrimaryIPStack).To(Equal(common.PrimaryIPStackV4))
			})

			It("should set PrimaryIPStack to IPv6 for IPv6-first dual stack", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:               &clusterID,
						OpenshiftVersion: "4.12.0", // Required for IPv6-primary support
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "2001:db8::/64"},
							{Cidr: "10.0.0.0/16"},
						},
						APIVips: []*models.APIVip{
							{IP: "2001:db8::1"},
							{IP: "10.0.1.1"},
						},
						IngressVips: []*models.IngressVip{
							{IP: "2001:db8::2"},
							{IP: "10.0.1.2"},
						},
						ServiceNetworks: []*models.ServiceNetwork{
							{Cidr: "2001:db8:1::/64"},
							{Cidr: "172.30.0.0/16"},
						},
						ClusterNetworks: []*models.ClusterNetwork{
							{Cidr: "2001:db8:2::/64"},
							{Cidr: "10.128.0.0/14"},
						},
					},
				}

				err := bm.setPrimaryIPStack(cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.PrimaryIPStack).ToNot(BeNil())
				Expect(*cluster.PrimaryIPStack).To(Equal(common.PrimaryIPStackV6))
			})

			It("should return error for inconsistent IP family order", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:               &clusterID,
						OpenshiftVersion: "4.12.0",
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "10.0.0.0/16"}, // IPv4 first
							{Cidr: "2001:db8::/64"},
						},
						APIVips: []*models.APIVip{
							{IP: "2001:db8::1"}, // IPv6 first - inconsistent!
							{IP: "10.0.1.1"},
						},
						ServiceNetworks: []*models.ServiceNetwork{
							{Cidr: "172.30.0.0/16"}, // IPv4 first
							{Cidr: "2001:db8:1::/64"},
						},
						ClusterNetworks: []*models.ClusterNetwork{
							{Cidr: "10.128.0.0/14"}, // IPv4 first
							{Cidr: "2001:db8:2::/64"},
						},
					},
				}

				err := bm.setPrimaryIPStack(cluster)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Inconsistent IP family order"))
			})
		})
	})

	Describe("orderClusterNetworks", func() {
		Context("IPv4 primary stack", func() {
			It("should not change networks already in IPv4-first order", func() {
				cluster := &common.Cluster{
					PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV4}[0],
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
							{Cidr: "10.128.0.0/14", HostPrefix: 23},
							{Cidr: "2001:db8:2::/64", HostPrefix: 64},
						},
					},
				}

				bm.orderClusterNetworks(cluster)

				// Networks should remain in IPv4-first order
				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(cluster.APIVips[0].IP)).To(Equal("10.0.1.1"))
				Expect(string(cluster.APIVips[1].IP)).To(Equal("2001:db8::1"))
				Expect(string(cluster.IngressVips[0].IP)).To(Equal("10.0.1.2"))
				Expect(string(cluster.IngressVips[1].IP)).To(Equal("2001:db8::2"))
				Expect(string(cluster.ServiceNetworks[0].Cidr)).To(Equal("172.30.0.0/16"))
				Expect(string(cluster.ServiceNetworks[1].Cidr)).To(Equal("2001:db8:1::/64"))
				Expect(string(cluster.ClusterNetworks[0].Cidr)).To(Equal("10.128.0.0/14"))
				Expect(string(cluster.ClusterNetworks[1].Cidr)).To(Equal("2001:db8:2::/64"))
			})

			It("should reorder IPv6-first networks to IPv4-first", func() {
				cluster := &common.Cluster{
					PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV4}[0],
					Cluster: models.Cluster{
						ID: &clusterID,
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "2001:db8::/64"},
							{Cidr: "10.0.0.0/16"},
						},
						APIVips: []*models.APIVip{
							{IP: "2001:db8::1"},
							{IP: "10.0.1.1"},
						},
					},
				}

				bm.orderClusterNetworks(cluster)

				// Networks should be reordered to IPv4-first
				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(cluster.APIVips[0].IP)).To(Equal("10.0.1.1"))
				Expect(string(cluster.APIVips[1].IP)).To(Equal("2001:db8::1"))
			})
		})

		Context("IPv6 primary stack", func() {
			It("should not change networks already in IPv6-first order", func() {
				cluster := &common.Cluster{
					PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
					Cluster: models.Cluster{
						ID: &clusterID,
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "2001:db8::/64"},
							{Cidr: "10.0.0.0/16"},
						},
						APIVips: []*models.APIVip{
							{IP: "2001:db8::1"},
							{IP: "10.0.1.1"},
						},
					},
				}

				bm.orderClusterNetworks(cluster)

				// Networks should remain in IPv6-first order
				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(cluster.APIVips[0].IP)).To(Equal("2001:db8::1"))
				Expect(string(cluster.APIVips[1].IP)).To(Equal("10.0.1.1"))
			})

			It("should reorder IPv4-first networks to IPv6-first", func() {
				cluster := &common.Cluster{
					PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
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
					},
				}

				bm.orderClusterNetworks(cluster)

				// Networks should be reordered to IPv6-first
				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(cluster.APIVips[0].IP)).To(Equal("2001:db8::1"))
				Expect(string(cluster.APIVips[1].IP)).To(Equal("10.0.1.1"))
			})
		})

		Context("No primary stack set", func() {
			It("should not change networks when PrimaryIPStack is nil", func() {
				cluster := &common.Cluster{
					PrimaryIPStack: nil,
					Cluster: models.Cluster{
						ID: &clusterID,
						MachineNetworks: []*models.MachineNetwork{
							{Cidr: "2001:db8::/64"},
							{Cidr: "10.0.0.0/16"},
						},
					},
				}

				bm.orderClusterNetworks(cluster)

				// Networks should remain unchanged
				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
			})
		})
	})

	Describe("Integration with GetClusterInternal", func() {
		It("should order networks when returning cluster via API", func() {
			// Create a dual-stack cluster in the database with IPv6-first order
			cluster := &common.Cluster{
				PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
				Cluster: models.Cluster{
					ID:               &clusterID,
					Name:             "test-cluster",
					OpenshiftVersion: "4.13.0",
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "2001:db8::/64"},
						{Cidr: "10.0.0.0/16"},
					},
					APIVips: []*models.APIVip{
						{IP: "2001:db8::1"},
						{IP: "10.0.1.1"},
					},
					IngressVips: []*models.IngressVip{
						{IP: "2001:db8::2"},
						{IP: "10.0.1.2"},
					},
					ServiceNetworks: []*models.ServiceNetwork{
						{Cidr: "2001:db8:1::/64"},
						{Cidr: "172.30.0.0/16"},
					},
					ClusterNetworks: []*models.ClusterNetwork{
						{Cidr: "2001:db8:2::/64", HostPrefix: 64},
						{Cidr: "10.128.0.0/14", HostPrefix: 23},
					},
				},
			}

			Expect(db.Create(cluster).Error).ToNot(HaveOccurred())

			// Call GetClusterInternal
			result, err := bm.GetClusterInternal(ctx, installer.V2GetClusterParams{
				ClusterID: clusterID,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify that networks are ordered according to IPv6 primary stack
			Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
			Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
			Expect(string(result.APIVips[0].IP)).To(Equal("2001:db8::1"))
			Expect(string(result.APIVips[1].IP)).To(Equal("10.0.1.1"))
			Expect(string(result.IngressVips[0].IP)).To(Equal("2001:db8::2"))
			Expect(string(result.IngressVips[1].IP)).To(Equal("10.0.1.2"))
			Expect(string(result.ServiceNetworks[0].Cidr)).To(Equal("2001:db8:1::/64"))
			Expect(string(result.ServiceNetworks[1].Cidr)).To(Equal("172.30.0.0/16"))
			Expect(string(result.ClusterNetworks[0].Cidr)).To(Equal("2001:db8:2::/64"))
			Expect(string(result.ClusterNetworks[1].Cidr)).To(Equal("10.128.0.0/14"))
		})
	})

	Describe("Integration with getCluster", func() {
		It("should order networks when getting cluster for install config", func() {
			// Create a dual-stack cluster in the database with IPv4-first order but IPv6 primary

			cluster := &common.Cluster{
				PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
				Cluster: models.Cluster{
					ID:               &clusterID,
					Name:             "test-cluster",
					OpenshiftVersion: "4.13.0",
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: "10.0.0.0/16"}, // Should be reordered
						{Cidr: "2001:db8::/64"},
					},
					APIVips: []*models.APIVip{
						{IP: "10.0.1.1"}, // Should be reordered
						{IP: "2001:db8::1"},
					},
				},
			}

			Expect(db.Create(cluster).Error).ToNot(HaveOccurred())

			// Call getCluster
			result, err := bm.getCluster(ctx, clusterID.String(), common.UseEagerLoading)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify that networks are reordered to IPv6-first according to primary stack
			Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
			Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
			Expect(string(result.APIVips[0].IP)).To(Equal("2001:db8::1"))
			Expect(string(result.APIVips[1].IP)).To(Equal("10.0.1.1"))
		})
	})
})
