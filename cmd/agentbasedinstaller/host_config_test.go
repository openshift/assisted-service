package agentbasedinstaller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	installerclient "github.com/openshift/assisted-service/client/installer"
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
			creds, err := loadFencingCredentials(tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(BeNil())
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
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

			creds, err := loadFencingCredentials(tempDir)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse fencing credentials file"))
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

			creds, err := loadFencingCredentials(tempDir)
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
			creds, err := loadFencingCredentials(tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).To(HaveLen(1))
			Expect(creds).To(HaveKey("master-0"))
		})
	})
})

var _ = Describe("applyHostConfigByHostname", func() {
	var (
		ctx            context.Context
		fakeTransport  *mockHostConfigTransport
		bmInventory    *client.AssistedInstall
		testHostID     strfmt.UUID
		testInfraEnvID strfmt.UUID
	)

	BeforeEach(func() {
		ctx = context.Background()

		testHostID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
		testInfraEnvID = strfmt.UUID("f789ab3f-4c96-51f1-9eda-93585266efc3")

		fakeTransport = NewMockHostConfigTransport()
		fakeInstallerClient := installerclient.New(fakeTransport, nil, nil)
		bmInventory = &client.AssistedInstall{
			Installer: fakeInstallerClient,
		}
	})

	Context("when fencing credentials map is nil", func() {
		It("should return nil without error", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  `{"hostname": "master-0"}`,
			}

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeTransport.updateHostCalled).To(BeFalse())
		})
	})

	Context("when host has no inventory", func() {
		It("should return nil without error", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  "",
			}

			fencingCreds := map[string]*models.FencingCredentialsParams{
				"master-0": {
					Address:  strPtr("redfish+https://example.com"),
					Username: strPtr("admin"),
					Password: strPtr("password"),
				},
			}

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, fencingCreds)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeTransport.updateHostCalled).To(BeFalse())
		})
	})

	Context("when host has empty hostname", func() {
		It("should return nil without error", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  `{"hostname": ""}`,
			}

			fencingCreds := map[string]*models.FencingCredentialsParams{
				"master-0": {
					Address:  strPtr("redfish+https://example.com"),
					Username: strPtr("admin"),
					Password: strPtr("password"),
				},
			}

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, fencingCreds)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeTransport.updateHostCalled).To(BeFalse())
		})
	})

	Context("when no matching hostname in credentials", func() {
		It("should return nil without error", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  `{"hostname": "worker-0"}`,
			}

			fencingCreds := map[string]*models.FencingCredentialsParams{
				"master-0": {
					Address:  strPtr("redfish+https://example.com"),
					Username: strPtr("admin"),
					Password: strPtr("password"),
				},
			}

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, fencingCreds)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeTransport.updateHostCalled).To(BeFalse())
		})
	})

	Context("when matching hostname found in credentials", func() {
		It("should call V2UpdateHost with fencing credentials", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  `{"hostname": "master-0"}`,
			}

			fencingCreds := map[string]*models.FencingCredentialsParams{
				"master-0": {
					Address:                 strPtr("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"),
					Username:                strPtr("admin"),
					Password:                strPtr("password"),
					CertificateVerification: strPtr("Disabled"),
				},
			}

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, fencingCreds)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeTransport.updateHostCalled).To(BeTrue())
			Expect(fakeTransport.lastUpdateParams).NotTo(BeNil())
			Expect(fakeTransport.lastUpdateParams.FencingCredentials).NotTo(BeNil())
			Expect(*fakeTransport.lastUpdateParams.FencingCredentials.Address).To(Equal("redfish+https://192.168.111.1:8000/redfish/v1/Systems/abc"))
			Expect(*fakeTransport.lastUpdateParams.FencingCredentials.Username).To(Equal("admin"))
			Expect(*fakeTransport.lastUpdateParams.FencingCredentials.Password).To(Equal("password"))
			Expect(*fakeTransport.lastUpdateParams.FencingCredentials.CertificateVerification).To(Equal("Disabled"))
		})
	})

	Context("when V2UpdateHost fails", func() {
		It("should return error", func() {
			testLogger, _ := test.NewNullLogger()
			host := &models.Host{
				ID:         &testHostID,
				InfraEnvID: testInfraEnvID,
				Inventory:  `{"hostname": "master-0"}`,
			}

			fencingCreds := map[string]*models.FencingCredentialsParams{
				"master-0": {
					Address:  strPtr("redfish+https://example.com"),
					Username: strPtr("admin"),
					Password: strPtr("password"),
				},
			}

			fakeTransport.SetUpdateError("API error")

			err := applyHostConfigByHostname(ctx, testLogger, bmInventory, host, fencingCreds)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update Host"))
		})
	})
})

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}

// mockHostConfigTransport is a mock transport for testing applyHostConfigByHostname
type mockHostConfigTransport struct {
	updateHostCalled bool
	lastUpdateParams *models.HostUpdateParams
	updateError      error
}

func NewMockHostConfigTransport() *mockHostConfigTransport {
	return &mockHostConfigTransport{}
}

func (m *mockHostConfigTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	switch v := op.Params.(type) {
	case *installerclient.V2UpdateHostParams:
		if m.updateError != nil {
			return nil, m.updateError
		}
		m.updateHostCalled = true
		m.lastUpdateParams = v.HostUpdateParams
		return &installerclient.V2UpdateHostCreated{
			Payload: &models.Host{},
		}, nil

	default:
		return nil, fmt.Errorf("[mockHostConfigTransport] unmanaged type: %T", v)
	}
}

func (m *mockHostConfigTransport) SetUpdateError(errMsg string) {
	m.updateError = fmt.Errorf("%s", errMsg)
}
