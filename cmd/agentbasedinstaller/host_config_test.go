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
		It("should return error for empty hostname", func() {
			content := `credentials:
- hostname: ""
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has empty hostname"))
			Expect(creds).To(BeNil())
		})

		It("should return error for missing address", func() {
			content := `credentials:
- hostname: master-0
  username: admin
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing required field: address"))
			Expect(creds).To(BeNil())
		})

		It("should return error for missing username", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  password: password123
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing required field: username"))
			Expect(creds).To(BeNil())
		})

		It("should return error for missing password", func() {
			content := `credentials:
- hostname: master-0
  address: redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc
  username: admin
`
			err := os.WriteFile(filepath.Join(tempDir, "fencing-credentials.yaml"), []byte(content), 0600)
			Expect(err).NotTo(HaveOccurred())

			creds, err := loadFencingCredentials(filepath.Join(tempDir, "fencing-credentials.yaml"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing required field: password"))
			Expect(creds).To(BeNil())
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

		It("should return error for duplicate hostnames", func() {
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
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate fencing credential for hostname: master-0"))
			Expect(creds).To(BeNil())
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
	Context("when credentials are nil", func() {
		It("should return false without modifying updateParams", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{}
			updateParams := &models.HostUpdateParams{}

			changed := applyFencingCredentials(testLogger, host, nil, updateParams)
			Expect(changed).To(BeFalse())
			Expect(updateParams.FencingCredentials).To(BeNil())
		})
	})

	Context("when host already has fencing credentials", func() {
		It("should return false without modifying updateParams", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				FencingCredentials: `{"address": "existing"}`,
			}
			creds := &models.FencingCredentialsParams{
				Address:  strPtr("redfish+https://example.com"),
				Username: strPtr("admin"),
				Password: strPtr("password"),
			}
			updateParams := &models.HostUpdateParams{}

			changed := applyFencingCredentials(testLogger, host, creds, updateParams)
			Expect(changed).To(BeFalse())
			Expect(updateParams.FencingCredentials).To(BeNil())
		})
	})

	Context("when credentials should be applied", func() {
		It("should set fencing credentials and return true", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{}
			creds := &models.FencingCredentialsParams{
				Address:                 strPtr("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"),
				Username:                strPtr("admin"),
				Password:                strPtr("password"),
				CertificateVerification: strPtr("Disabled"),
			}
			updateParams := &models.HostUpdateParams{}

			changed := applyFencingCredentials(testLogger, host, creds, updateParams)
			Expect(changed).To(BeTrue())
			Expect(updateParams.FencingCredentials).NotTo(BeNil())
			Expect(*updateParams.FencingCredentials.Address).To(Equal("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"))
			Expect(*updateParams.FencingCredentials.Username).To(Equal("admin"))
			Expect(*updateParams.FencingCredentials.Password).To(Equal("password"))
			Expect(*updateParams.FencingCredentials.CertificateVerification).To(Equal("Disabled"))
		})
	})
})

var _ = Describe("hostConfig.FencingCredentials", func() {
	Context("when hostConfig has no fencing credentials", func() {
		It("should return nil", func() {
			hc := hostConfig{
				configDir:    "/some/path",
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
				hostname:     "",
			}
			Expect(hc.FencingCredentials()).To(BeNil())
		})
	})

	Context("when hostConfig has fencing credentials", func() {
		It("should return credentials regardless of hostname", func() {
			expectedCreds := &models.FencingCredentialsParams{
				Address:  strPtr("redfish+https://example.com"),
				Username: strPtr("admin"),
				Password: strPtr("password"),
			}
			// MAC-based config with merged credentials (simulates post-merge state)
			hc := hostConfig{
				configDir:          "/some/path",
				macAddresses:       []string{"aa:bb:cc:dd:ee:ff"},
				hostname:           "", // Empty hostname - this is a MAC-based config
				fencingCredentials: expectedCreds,
			}
			Expect(hc.FencingCredentials()).To(Equal(expectedCreds))
		})
	})
})

var _ = Describe("findHostConfig with hostname matching", func() {
	var (
		testHostID strfmt.UUID
	)

	BeforeEach(func() {
		testHostID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
	})

	Context("when configs contain both MAC-based and hostname-based entries", func() {
		It("should merge fencing credentials when both MAC and hostname match", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
			}
			expectedCreds := &models.FencingCredentialsParams{
				Address: strPtr("redfish+https://example.com"),
			}
			hostnameConfig := &hostConfig{
				hostname:           "master-0",
				fencingCredentials: expectedCreds,
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			result := configs.findHostConfig(testHostID, inventory)
			// Should return MAC config with fencing credentials merged in
			Expect(result.configDir).To(Equal("/mac/path"))
			Expect(result.macAddresses).To(Equal([]string{"aa:bb:cc:dd:ee:ff"}))
			Expect(result.fencingCredentials).To(Equal(expectedCreds))
			Expect(result.hostID).To(Equal(testHostID))
		})

		It("should fall back to hostname matching when MAC doesn't match", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"11:22:33:44:55:66"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-0",
				fencingCredentials: &models.FencingCredentialsParams{
					Address: strPtr("redfish+https://example.com"),
				},
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			result := configs.findHostConfig(testHostID, inventory)
			Expect(result).To(Equal(hostnameConfig))
			Expect(result.hostID).To(Equal(testHostID))
		})

		It("should return nil when neither MAC nor hostname matches", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"11:22:33:44:55:66"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-1",
				fencingCredentials: &models.FencingCredentialsParams{
					Address: strPtr("redfish+https://example.com"),
				},
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			result := configs.findHostConfig(testHostID, inventory)
			Expect(result).To(BeNil())
		})

		It("should mark hostnameConfig as matched when merging", func() {
			macConfig := &hostConfig{
				configDir:    "/mac/path",
				macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
			}
			hostnameConfig := &hostConfig{
				hostname: "master-0",
				fencingCredentials: &models.FencingCredentialsParams{
					Address: strPtr("redfish+https://example.com"),
				},
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			_ = configs.findHostConfig(testHostID, inventory)

			// Both configs should be marked as matched
			Expect(macConfig.hostID).To(Equal(testHostID))
			Expect(hostnameConfig.hostID).To(Equal(testHostID))
		})

		It("should return independent struct that doesn't affect original macConfig", func() {
			originalCreds := &models.FencingCredentialsParams{
				Address: strPtr("original-address"),
			}
			macConfig := &hostConfig{
				configDir:          "/mac/path",
				macAddresses:       []string{"aa:bb:cc:dd:ee:ff"},
				fencingCredentials: originalCreds,
			}
			newCreds := &models.FencingCredentialsParams{
				Address: strPtr("new-address"),
			}
			hostnameConfig := &hostConfig{
				hostname:           "master-0",
				fencingCredentials: newCreds,
			}
			configs := HostConfigs{macConfig, hostnameConfig}

			inventory := &models.Inventory{
				Hostname: "master-0",
				Interfaces: []*models.Interface{
					{MacAddress: "aa:bb:cc:dd:ee:ff"},
				},
			}

			result := configs.findHostConfig(testHostID, inventory)

			// Returned config should have new credentials
			Expect(*result.fencingCredentials.Address).To(Equal("new-address"))
			// Original macConfig should still have original credentials
			Expect(*macConfig.fencingCredentials.Address).To(Equal("original-address"))
		})
	})
})

var _ = Describe("missingFencingCredentials", func() {
	var testLogger *test.Hook

	BeforeEach(func() {
		_, testLogger = test.NewNullLogger()
		_ = testLogger // Silence unused warning
	})

	Context("when fencing credentials are matched to a host", func() {
		It("should return empty list", func() {
			testLogger, _ := test.NewNullLogger()
			configs := HostConfigs{
				&hostConfig{
					hostname:           "master-0",
					fencingCredentials: &models.FencingCredentialsParams{Address: strPtr("redfish+https://example.com")},
					hostID:             strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2"), // Matched
				},
			}

			missing := configs.missingFencingCredentials(testLogger)
			Expect(missing).To(BeEmpty())
		})
	})

	Context("when fencing credentials have no matching hostname", func() {
		It("should return failure for unmatched fencing config", func() {
			testLogger, _ := test.NewNullLogger()
			configs := HostConfigs{
				&hostConfig{
					hostname:           "master-0",
					fencingCredentials: &models.FencingCredentialsParams{Address: strPtr("redfish+https://example.com")},
					hostID:             "", // Not matched - no host with this hostname
				},
			}

			missing := configs.missingFencingCredentials(testLogger)
			Expect(missing).To(HaveLen(1))
			Expect(missing[0].Hostname()).To(Equal("master-0"))
			Expect(missing[0].DescribeFailure()).To(Equal("Fencing credentials loaded but no host with matching hostname found"))
		})
	})

	Context("when MAC-based config without fencing is unmatched", func() {
		It("should not be reported by missingFencingCredentials", func() {
			testLogger, _ := test.NewNullLogger()
			configs := HostConfigs{
				&hostConfig{
					configDir:    "/mac/path",
					macAddresses: []string{"aa:bb:cc:dd:ee:ff"},
					hostID:       "", // Not matched
				},
			}

			// missingFencingCredentials should not report MAC-based configs
			missing := configs.missingFencingCredentials(testLogger)
			Expect(missing).To(BeEmpty())
		})
	})

	Context("when hostname config has no fencing credentials", func() {
		It("should not be reported", func() {
			testLogger, _ := test.NewNullLogger()
			configs := HostConfigs{
				&hostConfig{
					hostname:           "master-0",
					fencingCredentials: nil, // No credentials
					hostID:             "",
				},
			}

			missing := configs.missingFencingCredentials(testLogger)
			Expect(missing).To(BeEmpty())
		})
	})

	Context("with mixed matched and unmatched fencing configs", func() {
		It("should only report unmatched configs with credentials", func() {
			testLogger, _ := test.NewNullLogger()
			configs := HostConfigs{
				&hostConfig{
					hostname:           "master-0",
					fencingCredentials: &models.FencingCredentialsParams{Address: strPtr("redfish+https://example1.com")},
					hostID:             strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2"), // Matched
				},
				&hostConfig{
					hostname:           "master-1",
					fencingCredentials: &models.FencingCredentialsParams{Address: strPtr("redfish+https://example2.com")},
					hostID:             "", // Not matched
				},
			}

			missing := configs.missingFencingCredentials(testLogger)
			Expect(missing).To(HaveLen(1))
			Expect(missing[0].Hostname()).To(Equal("master-1"))
		})
	})
})

var _ = Describe("missingHost.DescribeFailure", func() {
	Context("when config is MAC-based without fencing", func() {
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

	Context("when config is hostname-based with fencing credentials", func() {
		It("should return fencing-specific message", func() {
			mh := missingHost{
				config: &hostConfig{
					hostname:           "master-0",
					fencingCredentials: &models.FencingCredentialsParams{Address: strPtr("redfish+https://example.com")},
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
