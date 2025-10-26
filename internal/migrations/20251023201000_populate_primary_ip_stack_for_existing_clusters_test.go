package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("populatePrimaryIPStackForExistingClusters", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID strfmt.UUID
		migration *gormigrate.Migration = populatePrimaryIPStackForExistingClusters()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("should populate PrimaryIPStack for dual-stack clusters (always IPv4)", func() {
		// Create a dual-stack cluster with IPv4-first configuration
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-ipv4-primary",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"}, // IPv4 first
					{Cidr: "2001:db8::/64"},    // IPv6 second
				},
				APIVips: []*models.APIVip{
					{IP: "192.168.127.100"}, // IPv4 first
					{IP: "2001:db8::1"},     // IPv6 second
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.127.101"}, // IPv4 first
					{IP: "2001:db8::2"},     // IPv6 second
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},   // IPv4 first
					{Cidr: "2001:db8:1::/64"}, // IPv6 second
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},   // IPv4 first
					{Cidr: "2001:db8:2::/64", HostPrefix: 64}, // IPv6 second
				},
				// PrimaryIPStack intentionally left nil to simulate existing cluster
			},
		}

		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run the migration
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		// Verify the PrimaryIPStack was set to IPv4 (consistent default)
		var primaryStack common.PrimaryIPStack
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal(common.PrimaryIPStackV4))
	})

	It("should populate PrimaryIPStack for dual-stack clusters regardless of order (always IPv4)", func() {
		// Create a dual-stack cluster with IPv6-first configuration
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-ipv6-first",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},    // IPv6 first
					{Cidr: "192.168.127.0/24"}, // IPv4 second
				},
				APIVips: []*models.APIVip{
					{IP: "2001:db8::1"},     // IPv6 first
					{IP: "192.168.127.100"}, // IPv4 second
				},
				IngressVips: []*models.IngressVip{
					{IP: "2001:db8::2"},     // IPv6 first
					{IP: "192.168.127.101"}, // IPv4 second
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "2001:db8:1::/64"}, // IPv6 first
					{Cidr: "172.30.0.0/16"},   // IPv4 second
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "2001:db8:2::/64", HostPrefix: 64}, // IPv6 first
					{Cidr: "10.128.0.0/14", HostPrefix: 23},   // IPv4 second
				},
			},
		}

		// Run the migration
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		// Verify the PrimaryIPStack was set to IPv4 (consistent default regardless of order)
		var primaryStack common.PrimaryIPStack
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal(common.PrimaryIPStackV4))
	})

	It("should leave PrimaryIPStack as nil for single-stack IPv4 clusters", func() {
		// Create a single-stack IPv4 cluster
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-single-stack-ipv4",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"}, // IPv4 only
				},
				APIVips: []*models.APIVip{
					{IP: "192.168.127.100"}, // IPv4 only
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.127.101"}, // IPv4 only
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"}, // IPv4 only
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23}, // IPv4 only
				},
			},
		}

		// Run the migration
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		// Verify the PrimaryIPStack remains nil
		var primaryStack *common.PrimaryIPStack
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(BeNil())
	})

	It("should handle clusters with inconsistent network configuration gracefully", func() {
		// Create a cluster with inconsistent dual-stack configuration
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-inconsistent",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},   // IPv4 first
					{Cidr: "2001:db8::/64"}, // IPv6 second
				},
				APIVips: []*models.APIVip{
					{IP: "2001:db8::1"}, // IPv6 first - inconsistent!
					{IP: "10.0.1.1"},    // IPv4 second
				},
			},
		}

		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run the migration - should succeed and set IPv4 as default
		Expect(migrateToBefore(db, migration.ID)).To(Succeed())
		Expect(migrateTo(db, migration.ID)).To(Succeed())

		// Verify the PrimaryIPStack was set to IPv4 (consistent default)
		var primaryStack common.PrimaryIPStack
		Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Select("primary_ip_stack").Scan(&primaryStack).Error).To(Succeed())
		Expect(primaryStack).To(Equal(common.PrimaryIPStackV4))
	})
})
