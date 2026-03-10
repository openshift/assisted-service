package hostutil

import (
	"encoding/base64"
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
	"github.com/vincent-petithory/dataurl"
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
			testCert := encodedSingleCAcert1

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
				actualRawCerts, err := getRawCertsFromEncodedBundle(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedRawCerts, err := getRawCertsFromEncodedBundle(testCert)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actualRawCerts).Should(Equal(expectedRawCerts))
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
				actualRawCerts, err := getRawCertsFromEncodedBundle(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedRawCerts, err := getRawCertsFromEncodedBundle(testCert)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actualRawCerts).Should(Equal(expectedRawCerts))
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should use api-int endpoint for HTTPS when host has ignition certificate in non-base64 data URL", func() {
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":             "test-cluster",
					"base_dns_domain":  "example.com",
					"api_vip_dns_name": "api.test-cluster.example.com",
				}).Error).ShouldNot(HaveOccurred())

				pemCert, err := base64.StdEncoding.DecodeString(testCert)
				Expect(err).ShouldNot(HaveOccurred())

				hostIgnitionOverride := `{
					"ignition": {
						"version": "3.2.0",
						"security": {
							"tls": {
								"certificateAuthorities": [{
									"source": "data:text/plain;charset=utf-8,` + dataurl.EscapeString(string(pemCert)) + `"
								}]
							}
						}
					}
				}`
				Expect(db.Model(&host).Update("ignition_config_overrides", hostIgnitionOverride).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())

				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())
				actualRawCerts, err := getRawCertsFromEncodedBundle(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedRawCerts, err := getRawCertsFromEncodedBundle(testCert)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actualRawCerts).Should(Equal(expectedRawCerts))
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

			It("should merge cluster and host certificates when both exist", func() {
				clusterCert := encodedSingleCAcert1
				hostCert := encodedSingleCAcert2

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
				Expect(*cert).ShouldNot(Equal(clusterCert))
				Expect(*cert).ShouldNot(Equal(hostCert))

				mergedBundle, err := base64.StdEncoding.DecodeString(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				mergedCerts, ok := common.ParsePemCerts(mergedBundle)
				Expect(ok).Should(BeTrue())
				Expect(mergedCerts).Should(HaveLen(2))

				expectedClusterBundle, err := base64.StdEncoding.DecodeString(clusterCert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedClusterCerts, ok := common.ParsePemCerts(expectedClusterBundle)
				Expect(ok).Should(BeTrue())
				Expect(expectedClusterCerts).Should(HaveLen(1))

				expectedHostBundle, err := base64.StdEncoding.DecodeString(hostCert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedHostCerts, ok := common.ParsePemCerts(expectedHostBundle)
				Expect(ok).Should(BeTrue())
				Expect(expectedHostCerts).Should(HaveLen(1))

				mergedRawCerts := []string{
					base64.StdEncoding.EncodeToString(mergedCerts[0].Raw),
					base64.StdEncoding.EncodeToString(mergedCerts[1].Raw),
				}
				Expect(mergedRawCerts).Should(ContainElements(
					base64.StdEncoding.EncodeToString(expectedClusterCerts[0].Raw),
					base64.StdEncoding.EncodeToString(expectedHostCerts[0].Raw),
				))
				Expect(url).Should(Equal("https://api-int.test-cluster.example.com:22623/config/worker"))
			})

			It("should remove duplicate certificates when cluster and host certificates are identical", func() {
				sharedCert := encodedSingleCAcert1

				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "test-cluster",
					"base_dns_domain":                  "example.com",
					"api_vip_dns_name":                 "api.test-cluster.example.com",
					"ignition_endpoint_ca_certificate": sharedCert,
				}).Error).ShouldNot(HaveOccurred())

				hostIgnitionOverride := `{
					"ignition": {
						"version": "3.2.0",
						"security": {
							"tls": {
								"certificateAuthorities": [{
									"source": "data:text/plain;charset=utf-8;base64,` + sharedCert + `"
								}]
							}
						}
					}
				}`
				Expect(db.Model(&host).Update("ignition_config_overrides", hostIgnitionOverride).Error).ShouldNot(HaveOccurred())

				_, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cert).ShouldNot(BeNil())

				mergedBundle, err := base64.StdEncoding.DecodeString(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				mergedCerts, ok := common.ParsePemCerts(mergedBundle)
				Expect(ok).Should(BeTrue())
				Expect(mergedCerts).Should(HaveLen(1))

				expectedBundle, err := base64.StdEncoding.DecodeString(sharedCert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedCerts, ok := common.ParsePemCerts(expectedBundle)
				Expect(ok).Should(BeTrue())
				Expect(expectedCerts).Should(HaveLen(1))

				Expect(base64.StdEncoding.EncodeToString(mergedCerts[0].Raw)).Should(
					Equal(base64.StdEncoding.EncodeToString(expectedCerts[0].Raw)))
			})

			It("should fail when cluster certificate is invalid and host certificate is valid", func() {
				hostCert := encodedSingleCAcert2
				clusterCert := "invalid"

				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "test-cluster",
					"base_dns_domain":                  "example.com",
					"api_vip_dns_name":                 "api.test-cluster.example.com",
					"ignition_endpoint_ca_certificate": clusterCert,
				}).Error).ShouldNot(HaveOccurred())

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
				Expect(err).Should(HaveOccurred())
				Expect(cert).Should(BeNil())
				Expect(url).Should(Equal(""))
			})

			It("should fail when host ignition certificate override is invalid", func() {
				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"name":                             "test-cluster",
					"base_dns_domain":                  "example.com",
					"api_vip_dns_name":                 "api.test-cluster.example.com",
					"ignition_endpoint_ca_certificate": encodedSingleCAcert1,
				}).Error).ShouldNot(HaveOccurred())
				Expect(db.Model(&host).Update("ignition_config_overrides", "{invalid-json").Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).Should(HaveOccurred())
				Expect(cert).Should(BeNil())
				Expect(url).Should(Equal(""))
			})

			It("should use custom endpoint with cluster certificate", func() {
				testCert := encodedSingleCAcert1
				customEndpoint := "https://custom.endpoint.com/ignition"

				Expect(db.Model(&cluster).Updates(map[string]interface{}{
					"ignition_endpoint_url":            customEndpoint,
					"ignition_endpoint_ca_certificate": testCert,
				}).Error).ShouldNot(HaveOccurred())

				url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(url).Should(Equal(customEndpoint + "/worker"))
				Expect(cert).ShouldNot(BeNil())
				actualRawCerts, err := getRawCertsFromEncodedBundle(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedRawCerts, err := getRawCertsFromEncodedBundle(testCert)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actualRawCerts).Should(Equal(expectedRawCerts))
			})

			It("should use custom endpoint with host certificate when no cluster certificate", func() {
				hostCert := encodedSingleCAcert2
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
				actualRawCerts, err := getRawCertsFromEncodedBundle(*cert)
				Expect(err).ShouldNot(HaveOccurred())
				expectedRawCerts, err := getRawCertsFromEncodedBundle(hostCert)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actualRawCerts).Should(Equal(expectedRawCerts))
			})

		})
	})
})

const encodedSingleCAcert1 = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUVORENDQXh5Z0F3SUJBZ0lKQU51bkkwRDY2MmNuTUEwR0NTcUdTSWIzRFFFQkN3VUFNSUdsTVFzd0NRWUQKVlFRR0V3SlZVekVYTUJVR0ExVUVDQXdPVG05eWRHZ2dRMkZ5YjJ4cGJtRXhFREFPQmdOVkJBY01CMUpoYkdWcApaMmd4RmpBVUJnTlZCQW9NRFZKbFpDQklZWFFzSUVsdVl5NHhFekFSQmdOVkJBc01DbEpsWkNCSVlYUWdTVlF4Ckd6QVpCZ05WQkFNTUVsSmxaQ0JJWVhRZ1NWUWdVbTl2ZENCRFFURWhNQjhHQ1NxR1NJYjNEUUVKQVJZU2FXNW0KYjNObFkwQnlaV1JvWVhRdVkyOXRNQ0FYRFRFMU1EY3dOakUzTXpneE1Wb1lEekl3TlRVd05qSTJNVGN6T0RFeApXakNCcFRFTE1Ba0dBMVVFQmhNQ1ZWTXhGekFWQmdOVkJBZ01EazV2Y25Sb0lFTmhjbTlzYVc1aE1SQXdEZ1lEClZRUUhEQWRTWVd4bGFXZG9NUll3RkFZRFZRUUtEQTFTWldRZ1NHRjBMQ0JKYm1NdU1STXdFUVlEVlFRTERBcFMKWldRZ1NHRjBJRWxVTVJzd0dRWURWUVFEREJKU1pXUWdTR0YwSUVsVUlGSnZiM1FnUTBFeElUQWZCZ2txaGtpRwo5dzBCQ1FFV0VtbHVabTl6WldOQWNtVmthR0YwTG1OdmJUQ0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQCkFEQ0NBUW9DZ2dFQkFMUXQ5T0pRaDZHQzVMVDFnODBxTmgwdTUwQlE0c1oveVo4YUVUeHQrNWxuUFZYNk1IS3oKYmZ3STZuTzFhTUc2ajliU3crNlVVeVBCSFA3OTYrRlQvcFRTK0swd3NEVjdjOVh2SG94SkJKSlUzOGNkTGtJMgpjL2k3bERxVGZUY2ZMTDJueVVCZDJmUURrMUIwZnhyc2toR0lJWjNpZlAxUHM0bHRUa3Y4aFJTb2IzVnROcVNvCkd4a0tmdkQyUEtqVFB4RFBXWXlydXk5aXJMWmlvTWZmaTNpL2dDdXQwWld0QXlPM01WSDVxV0YvZW5Ld2dQRVMKWDlwbytUZEN2UkIvUlVPYkJhTTc2MUVjckxTTTFHcUhOdWVTZnFuaG8zQWpMUTZkQm5QV2xvNjM4Wm0xVmViSwpCRUx5aGtMV01TRmtLd0RtbmUwalEwMlk0ZzA3NXZDS3ZDc0NBd0VBQWFOak1HRXdIUVlEVlIwT0JCWUVGSDdSCjR5QytVZWhJSVBldUw4WnF3M1B6YmdjWk1COEdBMVVkSXdRWU1CYUFGSDdSNHlDK1VlaElJUGV1TDhacXczUHoKYmdjWk1BOEdBMVVkRXdFQi93UUZNQU1CQWY4d0RnWURWUjBQQVFIL0JBUURBZ0dHTUEwR0NTcUdTSWIzRFFFQgpDd1VBQTRJQkFRQkROdkQyVm05c0E1QTlBbE9KUjgrZW41WHo5aFhjeEpCNXBoeGNaUThqRm9HMDRWc2h2ZDBlCkxFblVyTWNmRmdJWjRuak1LVFFDTTRaRlVQQWlleUx4NGY1Mkh1RG9wcDNlNUp5SU1mVytLRmNOSXBLd0NzYWsKb1NvS3RJVU9zVUpLN3FCVlp4Y3JJeWVRVjJxY1lPZVpodFM1d0JxSXdPQWhGd2xDRVQ3WmU1OFFIbVM0OHNsagpTOUswSkFjcHMyeGRuR3UwZmt6aFNReFk4R1BRTkZUbHI2cllsZDUrSUQvaEhlUzc2Z3EwWUczcTZSTFdSa0hmCjRlVGtSaml2QWxFeHJGektjbGpDNGF4S1Fsbk92VkF6eitHbTMyVTB4UEJGNEJ5ZVBWeENKVUh3MVRzeVRtZWwKUnhORXA3eUhvWGN3bitmWG5hK3Q1SldoMWd4VVp0eTMKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
const encodedSingleCAcert2 = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUQ2RENDQXRDZ0F3SUJBZ0lCRkRBTkJna3Foa2lHOXcwQkFRc0ZBRENCcFRFTE1Ba0dBMVVFQmhNQ1ZWTXgKRnpBVkJnTlZCQWdNRGs1dmNuUm9JRU5oY205c2FXNWhNUkF3RGdZRFZRUUhEQWRTWVd4bGFXZG9NUll3RkFZRApWUVFLREExU1pXUWdTR0YwTENCSmJtTXVNUk13RVFZRFZRUUxEQXBTWldRZ1NHRjBJRWxVTVJzd0dRWURWUVFECkRCSlNaV1FnU0dGMElFbFVJRkp2YjNRZ1EwRXhJVEFmQmdrcWhraUc5dzBCQ1FFV0VtbHVabTl6WldOQWNtVmsKYUdGMExtTnZiVEFlRncweE5URXdNVFF4TnpJNU1EZGFGdzAwTlRFd01EWXhOekk1TURkYU1FNHhFREFPQmdOVgpCQW9NQjFKbFpDQklZWFF4RFRBTEJnTlZCQXNNQkhCeWIyUXhLekFwQmdOVkJBTU1Ja2x1ZEdWeWJXVmthV0YwClpTQkRaWEowYVdacFkyRjBaU0JCZFhSb2IzSnBkSGt3Z2dFaU1BMEdDU3FHU0liM0RRRUJBUVVBQTRJQkR3QXcKZ2dFS0FvSUJBUURZcFZmZytqalEzNTQ2R0hGNnN4d01Pakl3cE9tZ0FYaUhTNHBnYUNtdStBUXdCczRyd3h2RgpTK1NzREhEVFZEdnB4SllCd0o2aDhTM0xLOXhrNzB5R3NPQXUzMEVxSVRqNlQrWlBiSkc2Qy8wSTV1a0VWSWVBCnhrZ1BlQ0JZaWlQd29OYy90ZTZSeTJ3bGFlSDlpVFZYOGZ4MzJ4cm9Ta2w2NVA1OS9kTXR0clF0U3VRWDhqTFMKNXJCU2pCZklMU3NhVXl3TkQzMTlFL0drcXZoNmxvM1RFYXg5cmhxYk5oMnMrMjZBZkJKb3VrWnN0ZzNUV2xJLwpwaTh2L0QzWkZEREVJT1hyUDBKRWZlOEVUbW04N1QxQ1BkUElaOSsvYzRBRFBIamRtZUJBSmRkbVQwSXNIOWU2CkdlYTJSL2ZRYVNySVFQVm1tLzBRWDJ3bFk0SmZ4eUxKQWdNQkFBR2plVEIzTUIwR0ExVWREZ1FXQkJRdzNnUlUKb1lZQ254SDZVUGtGY0tjb3dNQlAvREFmQmdOVkhTTUVHREFXZ0JSKzBlTWd2bEhvU0NEM3JpL0dhc056ODI0SApHVEFTQmdOVkhSTUJBZjhFQ0RBR0FRSC9BZ0VCTUE0R0ExVWREd0VCL3dRRUF3SUJoakFSQmdsZ2hrZ0JodmhDCkFRRUVCQU1DQVFZd0RRWUpLb1pJaHZjTkFRRUxCUUFEZ2dFQkFEd2FYTElPcW95UW9CVmNrOC81MkFqV3cxQ3YKYXRoOU5HVUVGUk9ZbTE1VmJBYUZtZVkyb1EwRVYzdFFSbTMyQzlxZTlSeFZVOERCRGpCdU55WWhMZzNrNi8xWgpKWGdndFNNdGZmcjVUODNieGdmaCt2TnhGN281b054RWdSVVlUQmk0YVY3djlMaURkMWI3WUFzVXdqNE5QV1laCmRidXlwRlNXQ29WN1JlTnQrMzdtdU1FWndpK3lHSVU5dWc4aExPcnZyaUVkVTNSWHQ1WE5JU01NdUM4SlVMZEUKM0dWem9OdGt6bnF2NXlTRWo0TTlXc2RCaUc2Ym00YUJZSU9FMFhLRTZRWXRsc2pUTUI5VVRYeG1sVXZERTB3Qwp6OVlZS2ZDMXZMeEwyd0FnTWhPQ2RLWk0rUWx1MXN0YjBCL0VGM294Yy9pWnJoRHZKTGppamJNcHBodz0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="

func getRawCertsFromEncodedBundle(encodedBundle string) ([]string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodedBundle)
	if err != nil {
		return nil, err
	}

	certs, ok := common.ParsePemCerts(decoded)
	if !ok {
		return nil, fmt.Errorf("failed to parse certificate bundle")
	}

	rawCerts := make([]string, 0, len(certs))
	for _, cert := range certs {
		rawCerts = append(rawCerts, base64.StdEncoding.EncodeToString(cert.Raw))
	}
	return rawCerts, nil
}

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
