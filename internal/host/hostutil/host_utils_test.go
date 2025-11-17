package hostutil

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("Installation Disk selection", func() {
	const (
		diskName      = "FirstDisk"
		diskId        = "/dev/disk/by-id/FirstDisk"
		otherDiskName = "SecondDisk"
		otherDiskId   = "/dev/disk/by-path/SecondDiskId"
	)

	for _, test := range []struct {
		testName                   string
		currentInstallationDisk    string
		inventoryDisks             []*models.Disk
		expectedInstallationDiskId string
	}{
		{testName: "No previous installation disk, no disks in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{},
			expectedInstallationDiskId: ""},
		{testName: "No previous installation disk, one disk in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "No previous installation disk, two disks in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}, {ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory still contains that disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory still contains that disk, but there's another",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}, {ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: diskId},
		{testName: `Previous installation disk is set, new inventory still contains that disk, but there's another
						disk with higher priority`,
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: otherDiskId, Name: otherDiskName}, {ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory doesn't contain any disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{},
			expectedInstallationDiskId: ""},
		{testName: "Previous installation disk is set, new inventory only contains a different disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: otherDiskId},
	} {
		test := test
		It(test.testName, func() {
			selectedDisk := DetermineInstallationDisk(test.inventoryDisks, test.currentInstallationDisk)

			if test.expectedInstallationDiskId == "" {
				Expect(selectedDisk).To(BeNil())
			} else {
				Expect(selectedDisk.ID).To(Equal(test.expectedInstallationDiskId))
			}
		})
	}
})

var _ = Describe("Validation", func() {
	It("Should not allow forbidden hostnames", func() {
		for _, hostName := range []string{
			"localhost",
			"localhost.localdomain",
			"localhost4",
			"localhost4.localdomain4",
			"localhost6",
			"localhost6.localdomain6",
		} {
			err := ValidateHostname(hostName)
			Expect(err).To(HaveOccurred())
		}
	})

	It("Should allow permitted hostnames", func() {
		for _, hostName := range []string{
			"foobar",
			"foobar.local",
			"arbitrary.hostname",
		} {
			err := ValidateHostname(hostName)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("Should not allow hostnames longer than 63 characters", func() {
		for _, hostName := range []string{
			"foobar.local.arbitrary.hostname.longer.than.64-characters.inthis.name",
			"foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234",
			"this-host.name-iss.exactly-64.characters.long.so.itt-should.fail",
		} {
			err := ValidateHostname(hostName)
			Expect(err).To(HaveOccurred())
		}
	})
})

var _ = Describe("Ignition endpoint URL generation", func() {
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var dbName string
	var id, clusterID, infraEnvID strfmt.UUID

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		apiVipDNSName := "test.com"
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, APIVipDNSName: &apiVipDNSName}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("using GetIgnitionEndpointAndCert function", func() {
		It("for host with custom MachineConfigPoolName", func() {
			Expect(db.Model(&host).Update("MachineConfigPoolName", "chocobomb").Error).ShouldNot(HaveOccurred())

			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://test.com:22624/config/chocobomb"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for cluster with custom IgnitionEndpoint", func() {
			customEndpoint := "https://foo.bar:33735/acme"
			Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())

			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal(customEndpoint + "/worker"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("failing for cluster with wrong IgnitionEndpoint", func() {
			customEndpoint := "https\\://foo.bar:33735/acme"
			Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())

			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal(""))
			Expect(cert).Should(BeNil())
			Expect(err).Should(HaveOccurred())
		})
		It("for host with master role", func() {
			Expect(db.Model(&host).Update("Role", "master").Error).ShouldNot(HaveOccurred())
			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://test.com:22624/config/master"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with auto-assing role defaults to worker", func() {
			Expect(db.Model(&host).Update("Role", "auto-assign").Error).ShouldNot(HaveOccurred())
			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://test.com:22624/config/worker"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with no customizations", func() {
			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://test.com:22624/config/worker"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with IPv4 API endpoint", func() {
			Expect(db.Model(&cluster).Update("api_vip_dns_name", "10.0.0.1").Error).ShouldNot(HaveOccurred())
			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://10.0.0.1:22624/config/worker"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with IPv6 API endpoint", func() {
			Expect(db.Model(&cluster).Update("api_vip_dns_name", "fe80::1").Error).ShouldNot(HaveOccurred())
			url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
			Expect(url).Should(Equal("http://[fe80::1]:22624/config/worker"))
			Expect(cert).Should(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("HTTPS scenarios for Day 2 workers", func() {
			testCert := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QgY2VydGlmaWNhdGUKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="

			It("should use api-int for day2/imported cluster with api.cluster.domain format", func() {
				// Simulate day2 imported cluster: has Name but no BaseDNSDomain
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "imported-cluster",
					"base_dns_domain":                  "", // Empty for imported clusters
					"api_vip_dns_name":                 "api.imported-cluster.example.com",
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				// Should convert api. to api-int. even without BaseDNSDomain
				Expect(url).Should(Equal("https://api-int.imported-cluster.example.com:22623/config/worker"))
			})

			It("should use api-int endpoint for HTTPS when cluster has ignition certificate", func() {
				// Setup cluster with DNS configuration and ignition certificate
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":             "test-cluster",
					"base_dns_domain":  "example.com",
					"api_vip_dns_name": "api.test-cluster.example.com",
				}).Error).ShouldNot(HaveOccurred())

				Expect(db.Model(&cluster).Update("ignition_endpoint_ca_certificate", testCert).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				Expect(*cert).Should(Equal(testCert))
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should use api-int endpoint for HTTPS when host has ignition certificate", func() {
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":             "test-cluster",
					"base_dns_domain":  "example.com",
					"api_vip_dns_name": "api.test-cluster.example.com",
				}).Error).ShouldNot(HaveOccurred())

				hostIgnitionOverride := `{
					"ignition": {
						"version": "3.2.0",
						"security": {
							"tls": {
								"certificateAuthorities": [{
									"source": "data:text/plain;charset=utf-8;base64,` + testCert + `"
								}]
							}
						}
					}
				}`
				Expect(db.Model(&host).Update("ignition_config_overrides", hostIgnitionOverride).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())

				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				Expect(*cert).Should(Equal(testCert))
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should still use configured hostname for HTTP when no certificate", func() {
				// Setup cluster with DNS configuration but no certificate
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":             "test-cluster",
					"base_dns_domain":  "example.com",
					"api_vip_dns_name": "api.test-cluster.example.com",
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).Should(BeNil())
				Expect(url).Should(Equal("http://api.test-cluster.example.com:22624/config/worker"))
			})

			It("should use IP address as-is for HTTPS when APIVipDNSName is an IP", func() {
				// Day2 clusters often use IP addresses instead of DNS
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "imported-cluster",
					"base_dns_domain":                  "", // Empty for imported clusters
					"api_vip_dns_name":                 "192.168.1.100",
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				// IP addresses should not be converted
				Expect(url).Should(Equal("https://192.168.1.100:22623/config/worker"))
			})

			It("should use custom DNS as-is for HTTPS when it doesn't start with 'api.'", func() {
				// Some clusters use custom DNS names
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "custom-cluster",
					"base_dns_domain":                  "",
					"api_vip_dns_name":                 "custom-api.mycompany.com",
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				// Custom DNS should not be converted
				Expect(url).Should(Equal("https://custom-api.mycompany.com:22623/config/worker"))
			})

			It("should not double-convert when APIVipDNSName already has api-int", func() {
				// Edge case: what if someone already has api-int in the name?
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "test-cluster",
					"base_dns_domain":                  "example.com",
					"api_vip_dns_name":                 "api-int.test-cluster.example.com",
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				// Should not double-convert
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should prefer cluster certificate over host certificate when both exist", func() {
				clusterCert := "Y2x1c3RlciBjZXJ0aWZpY2F0ZQo="
				hostCert := "aG9zdCBjZXJ0aWZpY2F0ZQo="

				// Setup cluster with certificate
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "test-cluster",
					"base_dns_domain":                  "example.com",
					"api_vip_dns_name":                 "api.test-cluster.example.com",
					"ignition_endpoint_ca_certificate": clusterCert,
				}).Error).ShouldNot(HaveOccurred())

				// Also add host certificate
				hostIgnitionOverride := `{
					"ignition": {
						"version": "3.2.0",
						"security": {
							"tls": {
								"certificateAuthorities": [{
									"source": "data:text/plain;charset=utf-8;base64,` + hostCert + `"
								}]
							}
						}
					}
				}`
				Expect(db.Model(&host).Update("ignition_config_overrides", hostIgnitionOverride).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				// Should use cluster certificate, not host certificate
				Expect(*cert).Should(Equal(clusterCert))
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should use custom endpoint with cluster certificate", func() {
				testCert := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCnRlc3QgY2VydGlmaWNhdGUKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
				customEndpoint := "https://custom.endpoint.com/ignition"

				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"ignition_endpoint_url":            customEndpoint,
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(url).Should(Equal(customEndpoint + "/worker"))
				Expect(cert).ShouldNot(BeNil())
				Expect(*cert).Should(Equal(testCert))
			})

			It("should use custom endpoint with host certificate when no cluster certificate", func() {
				hostCert := "aG9zdCBjZXJ0aWZpY2F0ZSBmb3IgY3VzdG9tIGVuZHBvaW50Cg=="
				customEndpoint := "https://private.ignition.server:8443/configs"

				// Setup custom endpoint without cluster certificate
				Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())

				// Add host certificate in ignition config
				hostIgnitionOverride := `{
					"ignition": {
						"version": "3.2.0",
						"security": {
							"tls": {
								"certificateAuthorities": [{
									"source": "data:text/plain;charset=utf-8;base64,` + hostCert + `"
								}]
							}
						}
					}
				}`
				Expect(db.Model(&host).Update("ignition_config_overrides", hostIgnitionOverride).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(url).Should(Equal(customEndpoint + "/worker"))
				Expect(cert).ShouldNot(BeNil())
				Expect(*cert).Should(Equal(hostCert))
			})

		})
	})
})

var _ = Describe("Validations", func() {
	Context("Role validity", func() {
		It("Day2 host should accept AutoAssign role", func() {
			isDay2Host := true
			Expect(IsRoleValid(models.HostRoleAutoAssign, isDay2Host)).Should(BeTrue())
		})
	})
})

var _ = Describe("Get Disks of Holder", func() {
	holder1 := models.Disk{DriveType: models.DriveTypeMultipath, Name: "dm-0"}
	holder2 := models.Disk{DriveType: models.DriveTypeMultipath, Name: "dm-1"}
	disksOfHolder1 := []models.Disk{{DriveType: models.DriveTypeISCSI, Name: "sda", Holders: "dm-0"}, {DriveType: models.DriveTypeISCSI, Name: "sdb", Holders: "dm-0"}}
	disksOfHolder2 := []models.Disk{{DriveType: models.DriveTypeFC, Name: "sda", Holders: "dm-1"}}
	disks := []*models.Disk{&holder1, &holder2, &disksOfHolder1[0], &disksOfHolder1[1], &disksOfHolder2[0]}

	It("All disks", func() {
		allDisks := GetAllDisksOfHolder(disks, &holder1)
		Expect(len(allDisks)).To(Equal(2))
		Expect(allDisks).Should(ContainElement(&disksOfHolder1[0]))
		Expect(allDisks).Should(ContainElement(&disksOfHolder1[1]))
	})
	It("Filtered by type", func() {
		filteredDisks := GetDisksOfHolderByType(disks, &holder2, models.DriveTypeFC)
		Expect(len(filteredDisks)).To(Equal(1))
		Expect(filteredDisks).Should(ContainElement(&disksOfHolder2[0]))
	})
})

var _ = DescribeTable("IsDiskEncryptionEnabledForRole", func(enabledOn string, role models.HostRole, expectedResult bool) {
	diskEncryption := models.DiskEncryption{
		EnableOn: &enabledOn,
	}
	isEnabled := IsDiskEncryptionEnabledForRole(diskEncryption, role)
	Expect(isEnabled).To(Equal(expectedResult))
},
	Entry("enabledOn all, role master", models.DiskEncryptionEnableOnAll, models.HostRoleMaster, true),
	Entry("enabledOn all, role bootstrap", models.DiskEncryptionEnableOnAll, models.HostRoleBootstrap, true),
	Entry("enabledOn all, role arbiter", models.DiskEncryptionEnableOnAll, models.HostRoleArbiter, true),
	Entry("enabledOn all, role worker", models.DiskEncryptionEnableOnAll, models.HostRoleWorker, true),
	Entry("enabledOn masters,arbiters,workers, role master", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleMaster, true),
	Entry("enabledOn masters,arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,arbiters,workers, role arbiter", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleArbiter, true),
	Entry("enabledOn masters,arbiters,workers, role worker", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn masters,arbiters, role master", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleMaster, true),
	Entry("enabledOn masters,arbiters, role bootstrap", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,arbiters, role arbiter", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleArbiter, true),
	Entry("enabledOn masters,arbiters, role worker", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleWorker, false),
	Entry("enabledOn masters,workers, role master", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleMaster, true),
	Entry("enabledOn masters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,workers, role arbiter", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleArbiter, false),
	Entry("enabledOn masters,workers, role worker", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn arbiters,workers, role master", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleMaster, false),
	Entry("enabledOn arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleBootstrap, false),
	Entry("enabledOn arbiters,workers, role arbiter", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleArbiter, true),
	Entry("enabledOn arbiters,workers, role worker", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn masters, role master", models.DiskEncryptionEnableOnMasters, models.HostRoleMaster, true),
	Entry("enabledOn masters, role bootstrap", models.DiskEncryptionEnableOnMasters, models.HostRoleBootstrap, true),
	Entry("enabledOn masters, role arbiter", models.DiskEncryptionEnableOnMasters, models.HostRoleArbiter, false),
	Entry("enabledOn masters, role worker", models.DiskEncryptionEnableOnMasters, models.HostRoleWorker, false),
	Entry("enabledOn arbiters, role master", models.DiskEncryptionEnableOnArbiters, models.HostRoleMaster, false),
	Entry("enabledOn arbiters, role bootstrap", models.DiskEncryptionEnableOnArbiters, models.HostRoleBootstrap, false),
	Entry("enabledOn arbiters, role arbiter", models.DiskEncryptionEnableOnArbiters, models.HostRoleArbiter, true),
	Entry("enabledOn arbiters, role worker", models.DiskEncryptionEnableOnArbiters, models.HostRoleWorker, false),
	Entry("enabledOn workers, role master", models.DiskEncryptionEnableOnWorkers, models.HostRoleMaster, false),
	Entry("enabledOn workers, role bootstrap", models.DiskEncryptionEnableOnWorkers, models.HostRoleBootstrap, false),
	Entry("enabledOn workers, role arbiter", models.DiskEncryptionEnableOnWorkers, models.HostRoleArbiter, false),
	Entry("enabledOn workers, role worker", models.DiskEncryptionEnableOnWorkers, models.HostRoleWorker, true),
	Entry("enabledOn none, role master", models.DiskEncryptionEnableOnNone, models.HostRoleMaster, false),
	Entry("enabledOn none, role bootstrap", models.DiskEncryptionEnableOnNone, models.HostRoleBootstrap, false),
	Entry("enabledOn none, role arbiter", models.DiskEncryptionEnableOnNone, models.HostRoleArbiter, false),
	Entry("enabledOn none, role worker", models.DiskEncryptionEnableOnNone, models.HostRoleWorker, false),
)

var _ = Describe("GetHostInstallationDisk", func() {
	var (
		hostId    strfmt.UUID
		diskId    = "/dev/disk/by-id/test-disk"
		diskPath  = "/dev/disk/by-path/test-disk"
		diskName  = "test-disk"
		validHost *models.Host
	)

	BeforeEach(func() {
		hostId = strfmt.UUID(uuid.New().String())
	})

	It("should return installation disk when found by disk ID", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).NotTo(HaveOccurred())
		Expect(disk).NotTo(BeNil())
		Expect(disk.ID).To(Equal(diskId))
		Expect(disk.Name).To(Equal(diskName))
	})

	It("should return installation disk when found by disk path", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, ByPath: diskPath},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: diskPath,
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).NotTo(HaveOccurred())
		Expect(disk).NotTo(BeNil())
		Expect(disk.ID).To(Equal(diskId))
		Expect(disk.Name).To(Equal(diskName))
	})

	It("should return installation disk when found by device name", func() {
		expectedFullName := fmt.Sprintf("/dev/%s", diskName)
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, Path: "/dev/sda"},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: expectedFullName,
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).NotTo(HaveOccurred())
		Expect(disk).NotTo(BeNil())
		Expect(disk.ID).To(Equal(diskId))
		Expect(disk.Name).To(Equal(diskName))
	})

	It("should return error when installation disk is not found - empty installation path", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when installation disk is not found - non-matching disk ID", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "/dev/disk/by-id/non-existent-disk",
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when installation disk is not found - non-matching device name", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, Path: "/dev/sda"},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: "/dev/non-existent-disk",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when installation disk is not found - non-matching disk path", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, Path: "/dev/sda", ByPath: "/dev/disk/by-path/existing-disk"},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: "/dev/disk/by-path/non-existent-disk",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when inventory has no disks", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when inventory is invalid JSON", func() {
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            "invalid json",
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(disk).To(BeNil())
	})
})

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HostUtil Tests")
}

var _ = BeforeSuite(func() {
	common.InitializeDBTest()
})

var _ = AfterSuite(func() {
	common.TerminateDBTest()
})
