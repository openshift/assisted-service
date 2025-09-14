package bminventory

//
//import (
//	"context"
//	"fmt"
//
//	"github.com/go-openapi/strfmt"
//	"github.com/go-openapi/swag"
//	"github.com/golang/mock/gomock"
//	"github.com/google/uuid"
//	. "github.com/onsi/ginkgo"
//	. "github.com/onsi/gomega"
//	"github.com/openshift/assisted-service/internal/common"
//	"github.com/openshift/assisted-service/internal/network"
//	"github.com/openshift/assisted-service/models"
//	"github.com/openshift/assisted-service/restapi/operations/installer"
//	"gorm.io/gorm"
//)
//
//var _ = Describe("Primary IP Stack Functionality", func() {
//	var (
//		bm                *bareMetalInventory
//		cfg               Config
//		db                *gorm.DB
//		ctx               = context.Background()
//		ctrl              *gomock.Controller
//		mockEvents        *eventsapi.MockHandler
//		mockSecretValidator *validations.MockPullSecretValidator
//		mockOperatorManager *operators.MockAPI
//		mockProviderRegistry *registry.MockProviderRegistry
//		mockInstallConfigBuilder *builder.MockInstallConfigBuilder
//		clusterID         strfmt.UUID
//	)
//
//	BeforeEach(func() {
//		db = common.PrepareTestDB()
//		ctrl = gomock.NewController(GinkgoT())
//		mockEvents = eventsapi.NewMockHandler(ctrl)
//		mockSecretValidator = validations.NewMockPullSecretValidator(ctrl)
//		mockOperatorManager = operators.NewMockAPI(ctrl)
//		mockProviderRegistry = registry.NewMockProviderRegistry(ctrl)
//		mockInstallConfigBuilder = builder.NewMockInstallConfigBuilder(ctrl)
//
//		cfg = Config{
//			DefaultClusterNetworkCidr:       "10.128.0.0/14",
//			DefaultServiceNetworkCidr:       "172.30.0.0/16",
//			DefaultClusterNetworkHostPrefix: 23,
//		}
//
//		bm = NewBareMetalInventory(db, nil, mockEvents, nil, cfg, mockOperatorManager, nil, nil, nil, nil, mockSecretValidator, nil, nil, nil, nil, nil, mockProviderRegistry, false, nil, mockInstallConfigBuilder, nil, nil, nil, nil, nil)
//
//		clusterID = strfmt.UUID(uuid.New().String())
//	})
//
//	AfterEach(func() {
//		ctrl.Finish()
//		common.DeleteTestDB(db, db)
//	})
//
//	Describe("setPrimaryIPStack", func() {
//		Context("Single stack clusters", func() {
//			It("should set PrimaryIPStack to nil for IPv4-only cluster", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						MachineNetworks: []*models.MachineNetwork{{Cidr: "10.0.0.0/16"}},
//						APIVips:         []*models.APIVip{{IP: "10.0.1.1"}},
//						IngressVips:     []*models.IngressVip{{IP: "10.0.1.2"}},
//						ServiceNetworks: []*models.ServiceNetwork{{Cidr: "172.30.0.0/16"}},
//						ClusterNetworks: []*models.ClusterNetwork{{Cidr: "10.128.0.0/14"}},
//					},
//				}
//
//				err := bm.setPrimaryIPStack(cluster)
//				Expect(err).ToNot(HaveOccurred())
//				Expect(cluster.PrimaryIPStack).To(BeNil())
//			})
//
//			It("should set PrimaryIPStack to nil for IPv6-only cluster", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						MachineNetworks: []*models.MachineNetwork{{Cidr: "2001:db8::/64"}},
//						APIVips:         []*models.APIVip{{IP: "2001:db8::1"}},
//						IngressVips:     []*models.IngressVip{{IP: "2001:db8::2"}},
//						ServiceNetworks: []*models.ServiceNetwork{{Cidr: "2001:db8:1::/64"}},
//						ClusterNetworks: []*models.ClusterNetwork{{Cidr: "2001:db8:2::/64"}},
//					},
//				}
//
//				err := bm.setPrimaryIPStack(cluster)
//				Expect(err).ToNot(HaveOccurred())
//				Expect(cluster.PrimaryIPStack).To(BeNil())
//			})
//		})
//
//		Context("Dual stack clusters", func() {
//			It("should set PrimaryIPStack to IPv4 for IPv4-first dual stack", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "10.0.0.0/16"},
//							{Cidr: "2001:db8::/64"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "10.0.1.1"},
//							{IP: "2001:db8::1"},
//						},
//						IngressVips: []*models.IngressVip{
//							{IP: "10.0.1.2"},
//							{IP: "2001:db8::2"},
//						},
//						ServiceNetworks: []*models.ServiceNetwork{
//							{Cidr: "172.30.0.0/16"},
//							{Cidr: "2001:db8:1::/64"},
//						},
//						ClusterNetworks: []*models.ClusterNetwork{
//							{Cidr: "10.128.0.0/14"},
//							{Cidr: "2001:db8:2::/64"},
//						},
//					},
//				}
//
//				err := bm.setPrimaryIPStack(cluster)
//				Expect(err).ToNot(HaveOccurred())
//				Expect(cluster.PrimaryIPStack).ToNot(BeNil())
//				Expect(*cluster.PrimaryIPStack).To(Equal(common.PrimaryIPStackV4))
//			})
//
//			It("should set PrimaryIPStack to IPv6 for IPv6-first dual stack", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "2001:db8::/64"},
//							{Cidr: "10.0.0.0/16"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "2001:db8::1"},
//							{IP: "10.0.1.1"},
//						},
//						IngressVips: []*models.IngressVip{
//							{IP: "2001:db8::2"},
//							{IP: "10.0.1.2"},
//						},
//						ServiceNetworks: []*models.ServiceNetwork{
//							{Cidr: "2001:db8:1::/64"},
//							{Cidr: "172.30.0.0/16"},
//						},
//						ClusterNetworks: []*models.ClusterNetwork{
//							{Cidr: "2001:db8:2::/64"},
//							{Cidr: "10.128.0.0/14"},
//						},
//					},
//				}
//
//				err := bm.setPrimaryIPStack(cluster)
//				Expect(err).ToNot(HaveOccurred())
//				Expect(cluster.PrimaryIPStack).ToNot(BeNil())
//				Expect(*cluster.PrimaryIPStack).To(Equal(common.PrimaryIPStackV6))
//			})
//
//			It("should return error for inconsistent IP family order", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "10.0.0.0/16"},    // IPv4 first
//							{Cidr: "2001:db8::/64"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "2001:db8::1"},      // IPv6 first - inconsistent!
//							{IP: "10.0.1.1"},
//						},
//					},
//				}
//
//				err := bm.setPrimaryIPStack(cluster)
//				Expect(err).To(HaveOccurred())
//				Expect(err.Error()).To(ContainSubstring("Inconsistent IP family order"))
//			})
//		})
//	})
//
//	Describe("orderClusterNetworks", func() {
//		Context("IPv4 primary stack", func() {
//			It("should not change networks already in IPv4-first order", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV4}[0],
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "10.0.0.0/16"},
//							{Cidr: "2001:db8::/64"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "10.0.1.1"},
//							{IP: "2001:db8::1"},
//						},
//						IngressVips: []*models.IngressVip{
//							{IP: "10.0.1.2"},
//							{IP: "2001:db8::2"},
//						},
//						ServiceNetworks: []*models.ServiceNetwork{
//							{Cidr: "172.30.0.0/16"},
//							{Cidr: "2001:db8:1::/64"},
//						},
//						ClusterNetworks: []*models.ClusterNetwork{
//							{Cidr: "10.128.0.0/14", HostPrefix: 23},
//							{Cidr: "2001:db8:2::/64", HostPrefix: 64},
//						},
//					},
//				}
//
//				bm.orderClusterNetworks(cluster)
//
//				// Networks should remain in IPv4-first order
//				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("10.0.0.0/16"))
//				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
//				Expect(string(cluster.APIVips[0].IP)).To(Equal("10.0.1.1"))
//				Expect(string(cluster.APIVips[1].IP)).To(Equal("2001:db8::1"))
//				Expect(string(cluster.IngressVips[0].IP)).To(Equal("10.0.1.2"))
//				Expect(string(cluster.IngressVips[1].IP)).To(Equal("2001:db8::2"))
//				Expect(string(cluster.ServiceNetworks[0].Cidr)).To(Equal("172.30.0.0/16"))
//				Expect(string(cluster.ServiceNetworks[1].Cidr)).To(Equal("2001:db8:1::/64"))
//				Expect(string(cluster.ClusterNetworks[0].Cidr)).To(Equal("10.128.0.0/14"))
//				Expect(string(cluster.ClusterNetworks[1].Cidr)).To(Equal("2001:db8:2::/64"))
//			})
//
//			It("should reorder IPv6-first networks to IPv4-first", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV4}[0],
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "2001:db8::/64"},
//							{Cidr: "10.0.0.0/16"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "2001:db8::1"},
//							{IP: "10.0.1.1"},
//						},
//					},
//				}
//
//				bm.orderClusterNetworks(cluster)
//
//				// Networks should be reordered to IPv4-first
//				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("10.0.0.0/16"))
//				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("2001:db8::/64"))
//				Expect(string(cluster.APIVips[0].IP)).To(Equal("10.0.1.1"))
//				Expect(string(cluster.APIVips[1].IP)).To(Equal("2001:db8::1"))
//			})
//		})
//
//		Context("IPv6 primary stack", func() {
//			It("should not change networks already in IPv6-first order", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "2001:db8::/64"},
//							{Cidr: "10.0.0.0/16"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "2001:db8::1"},
//							{IP: "10.0.1.1"},
//						},
//					},
//				}
//
//				bm.orderClusterNetworks(cluster)
//
//				// Networks should remain in IPv6-first order
//				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
//				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
//				Expect(string(cluster.APIVips[0].IP)).To(Equal("2001:db8::1"))
//				Expect(string(cluster.APIVips[1].IP)).To(Equal("10.0.1.1"))
//			})
//
//			It("should reorder IPv4-first networks to IPv6-first", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						PrimaryIPStack: &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "10.0.0.0/16"},
//							{Cidr: "2001:db8::/64"},
//						},
//						APIVips: []*models.APIVip{
//							{IP: "10.0.1.1"},
//							{IP: "2001:db8::1"},
//						},
//					},
//				}
//
//				bm.orderClusterNetworks(cluster)
//
//				// Networks should be reordered to IPv6-first
//				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
//				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
//				Expect(string(cluster.APIVips[0].IP)).To(Equal("2001:db8::1"))
//				Expect(string(cluster.APIVips[1].IP)).To(Equal("10.0.1.1"))
//			})
//		})
//
//		Context("No primary stack set", func() {
//			It("should not change networks when PrimaryIPStack is nil", func() {
//				cluster := &common.Cluster{
//					Cluster: models.Cluster{
//						ID: &clusterID,
//						PrimaryIPStack: nil,
//						MachineNetworks: []*models.MachineNetwork{
//							{Cidr: "2001:db8::/64"},
//							{Cidr: "10.0.0.0/16"},
//						},
//					},
//				}
//
//				bm.orderClusterNetworks(cluster)
//
//				// Networks should remain unchanged
//				Expect(string(cluster.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
//				Expect(string(cluster.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
//			})
//		})
//	})
//
//	Describe("Integration with GetClusterInternal", func() {
//		It("should order networks when returning cluster via API", func() {
//			// Create a dual-stack cluster in the database with IPv6-first order
//			cluster := &common.Cluster{
//				Cluster: models.Cluster{
//					ID:               &clusterID,
//					Name:             "test-cluster",
//					OpenshiftVersion: "4.13.0",
//					PrimaryIPStack:   &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
//					MachineNetworks: []*models.MachineNetwork{
//						{Cidr: "2001:db8::/64"},
//						{Cidr: "10.0.0.0/16"},
//					},
//					APIVips: []*models.APIVip{
//						{IP: "2001:db8::1"},
//						{IP: "10.0.1.1"},
//					},
//					IngressVips: []*models.IngressVip{
//						{IP: "2001:db8::2"},
//						{IP: "10.0.1.2"},
//					},
//					ServiceNetworks: []*models.ServiceNetwork{
//						{Cidr: "2001:db8:1::/64"},
//						{Cidr: "172.30.0.0/16"},
//					},
//					ClusterNetworks: []*models.ClusterNetwork{
//						{Cidr: "2001:db8:2::/64", HostPrefix: 64},
//						{Cidr: "10.128.0.0/14", HostPrefix: 23},
//					},
//				},
//			}
//
//			Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
//
//			// Call GetClusterInternal
//			result, err := bm.GetClusterInternal(ctx, installer.V2GetClusterParams{
//				ClusterID: clusterID,
//			})
//
//			Expect(err).ToNot(HaveOccurred())
//			Expect(result).ToNot(BeNil())
//
//			// Verify that networks are ordered according to IPv6 primary stack
//			Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
//			Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
//			Expect(string(result.APIVips[0].IP)).To(Equal("2001:db8::1"))
//			Expect(string(result.APIVips[1].IP)).To(Equal("10.0.1.1"))
//			Expect(string(result.IngressVips[0].IP)).To(Equal("2001:db8::2"))
//			Expect(string(result.IngressVips[1].IP)).To(Equal("10.0.1.2"))
//			Expect(string(result.ServiceNetworks[0].Cidr)).To(Equal("2001:db8:1::/64"))
//			Expect(string(result.ServiceNetworks[1].Cidr)).To(Equal("172.30.0.0/16"))
//			Expect(string(result.ClusterNetworks[0].Cidr)).To(Equal("2001:db8:2::/64"))
//			Expect(string(result.ClusterNetworks[1].Cidr)).To(Equal("10.128.0.0/14"))
//		})
//	})
//
//	Describe("Integration with getCluster", func() {
//		It("should order networks when getting cluster for install config", func() {
//			// Create a dual-stack cluster in the database with IPv4-first order but IPv6 primary
//			cluster := &common.Cluster{
//				Cluster: models.Cluster{
//					ID:               &clusterID,
//					Name:             "test-cluster",
//					OpenshiftVersion: "4.13.0",
//					PrimaryIPStack:   &[]common.PrimaryIPStack{common.PrimaryIPStackV6}[0],
//					MachineNetworks: []*models.MachineNetwork{
//						{Cidr: "10.0.0.0/16"},      // Should be reordered
//						{Cidr: "2001:db8::/64"},
//					},
//					APIVips: []*models.APIVip{
//						{IP: "10.0.1.1"},           // Should be reordered
//						{IP: "2001:db8::1"},
//					},
//				},
//			}
//
//			Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
//
//			// Call getCluster
//			result, err := bm.getCluster(ctx, clusterID.String(), common.UseEagerLoading)
//
//			Expect(err).ToNot(HaveOccurred())
//			Expect(result).ToNot(BeNil())
//
//			// Verify that networks are reordered to IPv6-first according to primary stack
//			Expect(string(result.MachineNetworks[0].Cidr)).To(Equal("2001:db8::/64"))
//			Expect(string(result.MachineNetworks[1].Cidr)).To(Equal("10.0.0.0/16"))
//			Expect(string(result.APIVips[0].IP)).To(Equal("2001:db8::1"))
//			Expect(string(result.APIVips[1].IP)).To(Equal("10.0.1.1"))
//		})
//	})
//
//	Describe("Cluster registration with primary IP stack determination", func() {
//		It("should determine and set primary IP stack during cluster registration", func() {
//			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
//			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
//			mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
//
//			params := installer.V2RegisterClusterParams{
//				NewClusterParams: &models.ClusterCreateParams{
//					Name:             swag.String("test-cluster"),
//					OpenshiftVersion: swag.String("4.13.0"),
//					PullSecret:       swag.String(`{"auths":{"cloud.openshift.com":{"auth":"test"}}}`),
//					MachineNetworks: []*models.MachineNetwork{
//						{Cidr: "2001:db8::/64"},    // IPv6 first
//						{Cidr: "10.0.0.0/16"},
//					},
//					APIVips: []*models.APIVip{
//						{IP: "2001:db8::1"},        // IPv6 first
//						{IP: "10.0.1.1"},
//					},
//					IngressVips: []*models.IngressVip{
//						{IP: "2001:db8::2"},        // IPv6 first
//						{IP: "10.0.1.2"},
//					},
//					ServiceNetworks: []*models.ServiceNetwork{
//						{Cidr: "2001:db8:1::/64"},  // IPv6 first
//						{Cidr: "172.30.0.0/16"},
//					},
//					ClusterNetworks: []*models.ClusterNetwork{
//						{Cidr: "2001:db8:2::/64", HostPrefix: 64}, // IPv6 first
//						{Cidr: "10.128.0.0/14", HostPrefix: 23},
//					},
//				},
//			}
//
//			reply := bm.V2RegisterCluster(ctx, params)
//			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
//
//			actual := reply.(*installer.V2RegisterClusterCreated)
//			Expect(actual.Payload.PrimaryIPStack).ToNot(BeNil())
//			Expect(*actual.Payload.PrimaryIPStack).To(Equal(common.PrimaryIPStackV6))
//		})
//	})
//
//	Describe("Cluster update with primary IP stack determination", func() {
//		var existingClusterID strfmt.UUID
//
//		BeforeEach(func() {
//			existingClusterID = strfmt.UUID(uuid.New().String())
//			cluster := &common.Cluster{
//				Cluster: models.Cluster{
//					ID:               &existingClusterID,
//					Name:             "existing-cluster",
//					OpenshiftVersion: "4.13.0",
//					PrimaryIPStack:   &[]common.PrimaryIPStack{common.PrimaryIPStackV4}[0],
//					MachineNetworks: []*models.MachineNetwork{
//						{Cidr: "10.0.0.0/16"},
//						{Cidr: "2001:db8::/64"},
//					},
//				},
//			}
//			Expect(db.Create(cluster).Error).ToNot(HaveOccurred())
//		})
//
//		It("should update primary IP stack when networks are changed", func() {
//			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
//
//			params := installer.V2UpdateClusterParams{
//				ClusterID: existingClusterID,
//				ClusterUpdateParams: &models.V2ClusterUpdateParams{
//					MachineNetworks: []*models.MachineNetwork{
//						{Cidr: "2001:db8::/64"},    // Change to IPv6 first
//						{Cidr: "10.0.0.0/16"},
//					},
//					APIVips: []*models.APIVip{
//						{IP: "2001:db8::1"},        // Change to IPv6 first
//						{IP: "10.0.1.1"},
//					},
//				},
//			}
//
//			reply := bm.V2UpdateCluster(ctx, params)
//			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
//
//			actual := reply.(*installer.V2UpdateClusterCreated)
//			Expect(actual.Payload.PrimaryIPStack).ToNot(BeNil())
//			Expect(*actual.Payload.PrimaryIPStack).To(Equal(common.PrimaryIPStackV6))
//		})
//
//		It("should not update primary IP stack when networks are not changed", func() {
//			params := installer.V2UpdateClusterParams{
//				ClusterID: existingClusterID,
//				ClusterUpdateParams: &models.V2ClusterUpdateParams{
//					Name: swag.String("updated-name"),
//				},
//			}
//
//			reply := bm.V2UpdateCluster(ctx, params)
//			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
//
//			actual := reply.(*installer.V2UpdateClusterCreated)
//			// Primary IP stack should remain unchanged
//			Expect(actual.Payload.PrimaryIPStack).ToNot(BeNil())
//			Expect(*actual.Payload.PrimaryIPStack).To(Equal(common.PrimaryIPStackV4))
//		})
//	})
//})
