package agentbasedinstaller

import (
	"os"
	"path/filepath"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = Describe("loadFencingCredentials", func() {
	var (
		tempDir string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "fencing-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("when fencing-credentials.yaml file does not exist", func() {
		It("should return nil map with no error", func() {
			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(BeNil())
		})
	})

	Context("when fencing-credentials.yaml has permission issues", func() {
		It("should return error for unreadable file", func() {
			if os.Geteuid() == 0 {
				Skip("Test skipped when running as root - chmod 0000 is ineffective for root user")
			}

			filePath := filepath.Join(tempDir, "fencing-credentials.yaml")
			err := os.WriteFile(filePath, []byte("credentials: []"), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Make file unreadable
			err = os.Chmod(filePath, 0000)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Chmod(filePath, 0600) }() // Cleanup

			creds, err := loadFencingCredentials(filePath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read fencing credentials file"))
			Expect(creds).To(BeNil())
		})
	})

	Context("when fencing-credentials.yaml has empty credentials array", func() {
		It("should return empty map for file with empty credentials array", func() {
			content := `credentials: []`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).NotTo(BeNil())
			Expect(creds).To(HaveLen(0))
		})
	})

	Context("when fencing-credentials.yaml has valid content", func() {
		It("should parse credentials with all fields", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
  certificateVerification: Disabled
- hostname: master-1
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/def
  username: admin2
  password: password456
  certificateVerification: Enabled
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(2))

			// Check master-0
			Expect(creds).To(HaveKey("master-0"))
			Expect(*creds["master-0"].Address).To(Equal("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"))
			Expect(*creds["master-0"].Username).To(Equal("admin"))
			Expect(*creds["master-0"].Password).To(Equal("password123"))
			Expect(*creds["master-0"].CertificateVerification).To(Equal("Disabled"))

			// Check master-1
			Expect(creds).To(HaveKey("master-1"))
			Expect(*creds["master-1"].Address).To(Equal("redfish+https://192.168.111.1:8000/redfish/v1/Systems/def"))
			Expect(*creds["master-1"].Username).To(Equal("admin2"))
			Expect(*creds["master-1"].Password).To(Equal("password456"))
			Expect(*creds["master-1"].CertificateVerification).To(Equal("Enabled"))
		})

		It("should parse credentials without optional certificateVerification", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(1))
			Expect(creds).To(HaveKey("master-0"))
			Expect(creds["master-0"].CertificateVerification).To(BeNil())
		})
	})

	Context("when fencing-credentials.yaml has invalid content", func() {
		It("should skip entries with empty hostname", func() {
			content := `credentials:
- hostname: ""
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/def
  username: admin
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			// Only the valid entry with hostname should be loaded
			Expect(creds).To(HaveLen(1))
			Expect(creds).To(HaveKey("master-0"))
		})

		It("should return error for invalid YAML", func() {
			content := `credentials:
  - hostname master-0
    address: invalid yaml`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse fencing credentials file"))
			Expect(creds).To(BeNil())
		})

		It("should return error for unknown fields (strict parsing)", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
  unknownField: somevalue
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse fencing credentials file"))
			Expect(creds).To(BeNil())
		})

		It("should use last entry for duplicate hostnames", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
- hostname: master-0
  address: redfish+https://192.168.111.2:8000/redfish/v1/Systems/def
  username: admin2
  password: password456
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(1))
			Expect(creds).To(HaveKey("master-0"))
			// The last entry should be used
			Expect(*creds["master-0"].Address).To(Equal("redfish+https://192.168.111.2:8000/redfish/v1/Systems/def"))
		})
	})

	Context("YAML round-trip compatibility with installer output", func() {
		It("should correctly parse installer-generated YAML format", func() {
			// This YAML matches the exact format the installer produces
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password
  certificateVerification: Disabled
- hostname: master-1
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/def
  username: admin
  password: password
  certificateVerification: Enabled
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(2))
		})

		It("should handle field case insensitively (YAML convention)", func() {
			// YAML field matching is case-insensitive in sigs.k8s.io/yaml
			// This is a feature, not a bug - it provides robustness
			content := `credentials:
- hostName: master-0
  Address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  USERNAME: admin
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Case-insensitive matching means this parses successfully
			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(1))
			Expect(creds).To(HaveKey("master-0"))
		})
	})
})

var _ = Describe("applyFencingCredentials", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "apply-fencing-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("when config has no fencing credentials (MAC-based config)", func() {
		It("should return false without modifying updateParams", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{}
			config := &hostConfig{
				// MAC-based config - no hostname means FencingCredentials() returns nil
				configDir:    tempDir,
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
			}
			updateParams := &models.HostUpdateParams{}

			applied, err := applyFencingCredentials(testLogger, host, config, updateParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeFalse())
			Expect(updateParams.FencingCredentials).To(BeNil())
		})
	})

	Context("when host already has fencing credentials", func() {
		It("should return false without modifying updateParams", func() {
			// Create fencing credentials file
			content := `credentials:
- hostname: master-0
  address: redfish+https://example.com
  username: admin
  password: password
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				FencingCredentials: `{"address": "existing"}`,
			}
			config := &hostConfig{
				configDir: tempDir,
				hostname:  "master-0",
			}
			updateParams := &models.HostUpdateParams{}

			applied, err := applyFencingCredentials(testLogger, host, config, updateParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeFalse())
			Expect(updateParams.FencingCredentials).To(BeNil())
		})
	})

	Context("when credentials should be applied", func() {
		It("should set fencing credentials and return true", func() {
			// Create fencing credentials file
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password
  certificateVerification: Disabled
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			testLogger, _ := test.NewNullLogger()
			host := &models.Host{}
			config := &hostConfig{
				configDir: tempDir,
				hostname:  "master-0",
			}
			updateParams := &models.HostUpdateParams{}

			applied, err := applyFencingCredentials(testLogger, host, config, updateParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeTrue())
			Expect(updateParams.FencingCredentials).NotTo(BeNil())
			Expect(*updateParams.FencingCredentials.Address).To(Equal("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"))
			Expect(*updateParams.FencingCredentials.Username).To(Equal("admin"))
			Expect(*updateParams.FencingCredentials.Password).To(Equal("password"))
			Expect(*updateParams.FencingCredentials.CertificateVerification).To(Equal("Disabled"))
		})
	})
})

var _ = Describe("findHostConfigs with hostname matching", func() {
	var (
		testHostID strfmt.UUID
	)

	BeforeEach(func() {
		testHostID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
	})

	Context("when configs contain both MAC-based and hostname-based entries", func() {
		It("should return both MAC and hostname configs when both match", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-0",
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			results := configs.findHostConfigs(testHostID, inventory)
			// Should return both configs
			Expect(results).To(HaveLen(2))
			Expect(results[0].configDir).To(Equal("/mac/path"))
			Expect(results[0].macAddresses).To(Equal([]string{"aa:bb:cc:dd:ee:ff"}))
			Expect(results[0].hostID).To(Equal(testHostID))
			Expect(results[1].hostname).To(Equal("master-0"))
			Expect(results[1].hostID).To(Equal(testHostID))
		})

		It("should return only hostname config when MAC doesn't match", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"11:22:33:44:55:66"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-0",
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			results := configs.findHostConfigs(testHostID, inventory)
			Expect(results).To(HaveLen(1))
			Expect(results[0]).To(Equal(hostnameConfig))
			Expect(results[0].hostID).To(Equal(testHostID))
		})

		It("should return only MAC config when hostname doesn't match", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-1",
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			results := configs.findHostConfigs(testHostID, inventory)
			Expect(results).To(HaveLen(1))
			Expect(results[0]).To(Equal(macConfig))
			Expect(results[0].hostID).To(Equal(testHostID))
		})

		It("should return empty slice when neither MAC nor hostname matches", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"11:22:33:44:55:66"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-1",
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			results := configs.findHostConfigs(testHostID, inventory)
			Expect(results).To(BeEmpty())
		})
	})
})

var _ = Describe("missingHost.DescribeFailure", func() {
	Context("when config is MAC-based", func() {
		It("should return 'Host not registered'", func() {
			mh := missingHost{
				config: &hostConfig{
					configDir:    "/mac/path",
					macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
				},
			}
			Expect(mh.DescribeFailure()).To(Equal("Host not registered"))
		})
	})

	Context("when config is hostname-based", func() {
		It("should return fencing-specific message", func() {
			mh := missingHost{
				config: &hostConfig{
					hostname: "master-0",
				},
			}
			Expect(mh.DescribeFailure()).To(Equal("Fencing credentials loaded but no host with matching hostname found"))
		})
	})
})

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}
