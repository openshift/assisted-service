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

var _ = Describe("convertNetworkColumnsToNativeTypes", func() {
	var (
		db        *gorm.DB
		dbName    string
		migration *gormigrate.Migration = convertNetworkColumnsToNativeTypes()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	// Helper to revert columns to text for testing
	revertColumnsToText := func() {
		tables := []struct {
			table  string
			column string
		}{
			{"api_vips", "ip"},
			{"ingress_vips", "ip"},
			{"cluster_networks", "cidr"},
			{"service_networks", "cidr"},
			{"machine_networks", "cidr"},
		}

		for _, t := range tables {
			var dataType string
			err := db.Raw(`
				SELECT data_type FROM information_schema.columns 
				WHERE table_name = ? AND column_name = ?
			`, t.table, t.column).Scan(&dataType).Error
			Expect(err).ToNot(HaveOccurred())

			if dataType == "inet" || dataType == "cidr" {
				err = db.Exec(`
					ALTER TABLE ` + t.table + `
					ALTER COLUMN ` + t.column + ` TYPE text
					USING ` + t.column + `::text
				`).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}
	}

	It("should convert IP columns to inet type", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Create cluster with VIPs
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-ip",
				APIVips: []*models.APIVip{
					{IP: "192.168.1.100"},
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.1.101"},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify column types
		var apiVipType, ingressVipType string
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'api_vips' AND column_name = 'ip'`).Scan(&apiVipType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'ingress_vips' AND column_name = 'ip'`).Scan(&ingressVipType).Error).To(Succeed())
		Expect(apiVipType).To(Equal("inet"))
		Expect(ingressVipType).To(Equal("inet"))

		// Verify values are preserved (use host() to strip the /32 mask that inet adds)
		var apiVip, ingressVip string
		Expect(db.Raw(`SELECT host(ip) FROM api_vips WHERE cluster_id = ?`, clusterID.String()).Scan(&apiVip).Error).To(Succeed())
		Expect(db.Raw(`SELECT host(ip) FROM ingress_vips WHERE cluster_id = ?`, clusterID.String()).Scan(&ingressVip).Error).To(Succeed())
		Expect(apiVip).To(Equal("192.168.1.100"))
		Expect(ingressVip).To(Equal("192.168.1.101"))
	})

	It("should convert CIDR columns to cidr type and normalize network addresses", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Create cluster with networks (some with non-zero host bits)
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-cidr",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify column types
		var machineNetType, serviceNetType, clusterNetType string
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'machine_networks' AND column_name = 'cidr'`).Scan(&machineNetType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'service_networks' AND column_name = 'cidr'`).Scan(&serviceNetType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'cluster_networks' AND column_name = 'cidr'`).Scan(&clusterNetType).Error).To(Succeed())
		Expect(machineNetType).To(Equal("cidr"))
		Expect(serviceNetType).To(Equal("cidr"))
		Expect(clusterNetType).To(Equal("cidr"))

		// Verify values are preserved (normalized)
		var machineNet, serviceNet, clusterNet string
		Expect(db.Raw(`SELECT cidr::text FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&machineNet).Error).To(Succeed())
		Expect(db.Raw(`SELECT cidr::text FROM service_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&serviceNet).Error).To(Succeed())
		Expect(db.Raw(`SELECT cidr::text FROM cluster_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&clusterNet).Error).To(Succeed())
		Expect(machineNet).To(Equal("192.168.1.0/24"))
		Expect(serviceNet).To(Equal("172.30.0.0/16"))
		Expect(clusterNet).To(Equal("10.128.0.0/14"))
	})

	It("should normalize CIDRs with non-zero host bits", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Insert data with non-zero host bits using raw SQL
		Expect(db.Exec(`INSERT INTO clusters (id, name) VALUES (?, 'test')`, clusterID.String()).Error).ToNot(HaveOccurred())
		// 192.168.1.100/24 should normalize to 192.168.1.0/24
		Expect(db.Exec(`INSERT INTO machine_networks (cidr, cluster_id) VALUES ('192.168.1.100/24', ?)`, clusterID.String()).Error).ToNot(HaveOccurred())
		// 10.128.5.0/14 should normalize to 10.128.0.0/14
		Expect(db.Exec(`INSERT INTO cluster_networks (cidr, cluster_id, host_prefix) VALUES ('10.128.5.0/14', ?, 23)`, clusterID.String()).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify values are normalized
		var machineNet, clusterNet string
		Expect(db.Raw(`SELECT cidr::text FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&machineNet).Error).To(Succeed())
		Expect(db.Raw(`SELECT cidr::text FROM cluster_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&clusterNet).Error).To(Succeed())
		Expect(machineNet).To(Equal("192.168.1.0/24"))
		Expect(clusterNet).To(Equal("10.128.0.0/14"))
	})

	It("should handle IPv6 addresses", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Create cluster with IPv6 addresses
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-ipv6",
				APIVips: []*models.APIVip{
					{IP: "2001:db8::1"},
				},
				IngressVips: []*models.IngressVip{
					{IP: "2001:db8::2"},
				},
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "fd00::/112"},
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "fd01::/48", HostPrefix: 64},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify values are preserved (use host() to strip the /128 mask that inet adds)
		var apiVip string
		Expect(db.Raw(`SELECT host(ip) FROM api_vips WHERE cluster_id = ?`, clusterID.String()).Scan(&apiVip).Error).To(Succeed())
		Expect(apiVip).To(Equal("2001:db8::1"))
	})

	It("should delete rows with empty values", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Insert data including empty values using raw SQL
		// Note: api_vips.ip has NOT NULL constraint, so we test with machine_networks
		Expect(db.Exec(`INSERT INTO clusters (id, name) VALUES (?, 'test')`, clusterID.String()).Error).ToNot(HaveOccurred())
		Expect(db.Exec(`INSERT INTO machine_networks (cidr, cluster_id) VALUES ('192.168.1.0/24', ?)`, clusterID.String()).Error).ToNot(HaveOccurred())
		Expect(db.Exec(`INSERT INTO machine_networks (cidr, cluster_id) VALUES ('', ?)`, clusterID.String()).Error).ToNot(HaveOccurred())

		// Verify we have 2 rows before migration
		var countBefore int64
		Expect(db.Raw(`SELECT COUNT(*) FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&countBefore).Error).To(Succeed())
		Expect(countBefore).To(Equal(int64(2)))

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify only valid row remains
		var countAfter int64
		Expect(db.Raw(`SELECT COUNT(*) FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&countAfter).Error).To(Succeed())
		Expect(countAfter).To(Equal(int64(1)))

		// Verify the valid value is preserved
		var cidr string
		Expect(db.Raw(`SELECT cidr::text FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&cidr).Error).To(Succeed())
		Expect(cidr).To(Equal("192.168.1.0/24"))
	})

	It("should be idempotent - skip already converted columns", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Create cluster with networks
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-idempotent",
				APIVips: []*models.APIVip{
					{IP: "192.168.1.100"},
				},
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration twice - should succeed both times
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Run again - should be idempotent
		gm2 := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm2.Migrate()).To(Succeed())

		// Verify column types are still correct
		var apiVipType, machineNetType string
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'api_vips' AND column_name = 'ip'`).Scan(&apiVipType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'machine_networks' AND column_name = 'cidr'`).Scan(&machineNetType).Error).To(Succeed())
		Expect(apiVipType).To(Equal("inet"))
		Expect(machineNetType).To(Equal("cidr"))
	})

	It("should rollback from native types to text", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Create cluster with networks
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-rollback",
				APIVips: []*models.APIVip{
					{IP: "192.168.1.100"},
				},
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify columns are native types
		var apiVipType, machineNetType string
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'api_vips' AND column_name = 'ip'`).Scan(&apiVipType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'machine_networks' AND column_name = 'cidr'`).Scan(&machineNetType).Error).To(Succeed())
		Expect(apiVipType).To(Equal("inet"))
		Expect(machineNetType).To(Equal("cidr"))

		// Run rollback
		Expect(gm.RollbackMigration(migration)).To(Succeed())

		// Verify columns are back to text
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'api_vips' AND column_name = 'ip'`).Scan(&apiVipType).Error).To(Succeed())
		Expect(db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'machine_networks' AND column_name = 'cidr'`).Scan(&machineNetType).Error).To(Succeed())
		Expect(apiVipType).To(Equal("text"))
		Expect(machineNetType).To(Equal("text"))

		// Verify values are preserved as text
		// Note: inet stores IPs with /32 mask, so after rollback to text it will be "192.168.1.100/32"
		var ip, cidr string
		Expect(db.Raw(`SELECT ip FROM api_vips WHERE cluster_id = ?`, clusterID.String()).Scan(&ip).Error).To(Succeed())
		Expect(db.Raw(`SELECT cidr FROM machine_networks WHERE cluster_id = ?`, clusterID.String()).Scan(&cidr).Error).To(Succeed())
		Expect(ip).To(Equal("192.168.1.100/32"))
		Expect(cidr).To(Equal("192.168.1.0/24"))
	})

	It("should handle dual-stack clusters", func() {
		clusterID := strfmt.UUID(uuid.New().String())

		// Revert columns to text first
		revertColumnsToText()

		// Create dual-stack cluster
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:   &clusterID,
				Name: "test-cluster-dual-stack",
				APIVips: []*models.APIVip{
					{IP: "192.168.1.100"},
					{IP: "2001:db8::100"},
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.1.101"},
					{IP: "2001:db8::101"},
				},
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
					{Cidr: "2001:db8::/64"},
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "fd00::/112"},
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "fd01::/48", HostPrefix: 64},
				},
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Run migration
		gm := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{migration})
		Expect(gm.Migrate()).To(Succeed())

		// Verify all VIPs are preserved
		var apiVips []string
		Expect(db.Raw(`SELECT ip::text FROM api_vips WHERE cluster_id = ? ORDER BY ip`, clusterID.String()).Scan(&apiVips).Error).To(Succeed())
		Expect(apiVips).To(HaveLen(2))

		// Verify all networks are preserved
		var machineNets []string
		Expect(db.Raw(`SELECT cidr::text FROM machine_networks WHERE cluster_id = ? ORDER BY cidr`, clusterID.String()).Scan(&machineNets).Error).To(Succeed())
		Expect(machineNets).To(HaveLen(2))
	})
})
