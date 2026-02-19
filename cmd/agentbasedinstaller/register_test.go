package agentbasedinstaller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	installerclient "github.com/openshift/assisted-service/client/installer"
	manifestsclient "github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = Describe("ApplyInstallConfigOverrides", func() {
	var (
		ctx            context.Context
		fakeTransport  *mockInstallConfigTransport
		bmInventory    *client.AssistedInstall
		cluster        *models.Cluster
		clusterID      strfmt.UUID
		tempDir        string
		aciFilePath    string
		aciWithOveride string
		aciNoOverride  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeLogger, _ := test.NewNullLogger()
		_ = fakeLogger

		clusterID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
		cluster = &models.Cluster{
			ID:                     &clusterID,
			InstallConfigOverrides: "",
		}

		fakeTransport = NewMockInstallConfigTransport()
		fakeInstallerClient := installerclient.New(fakeTransport, nil, nil)
		bmInventory = &client.AssistedInstall{
			Installer: fakeInstallerClient,
		}

		// Create temp directory for test manifests
		var err error
		tempDir, err = os.MkdirTemp("", "abi-test-*")
		Expect(err).NotTo(HaveOccurred())

		aciFilePath = filepath.Join(tempDir, "agentclusterinstall.yaml")

		// ACI manifest with installConfig overrides
		aciWithOveride = `apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  annotations:
    agent-install.openshift.io/install-config-overrides: '{"fips": true}'
  name: test-cluster
  namespace: test-namespace
spec:
  clusterDeploymentRef:
    name: test-cluster
  imageSetRef:
    name: openshift-v4.15.0
  networking:
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    machineNetwork:
    - cidr: 192.168.111.0/24
    serviceNetwork:
    - 172.30.0.0/16
  provisionRequirements:
    controlPlaneAgents: 1
  sshPublicKey: ssh-rsa test-key
`

		// ACI manifest without installConfig overrides
		aciNoOverride = `apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  name: test-cluster
  namespace: test-namespace
spec:
  clusterDeploymentRef:
    name: test-cluster
  imageSetRef:
    name: openshift-v4.15.0
  networking:
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    machineNetwork:
    - cidr: 192.168.111.0/24
    serviceNetwork:
    - 172.30.0.0/16
  provisionRequirements:
    controlPlaneAgents: 1
  sshPublicKey: ssh-rsa test-key
`
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("when installConfig overrides are present", func() {
		It("should apply overrides to cluster without overrides", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Cluster starts with no overrides
			cluster.InstallConfigOverrides = ""

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster).NotTo(BeNil())
			Expect(updatedCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeTrue())
			Expect(fakeTransport.lastInstallConfigParams).To(Equal(`{"fips": true}`))
		})

		It("should not apply overrides if already correctly applied", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Cluster already has the correct overrides
			cluster.InstallConfigOverrides = `{"fips": true}`

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster).To(BeNil()) // No update needed
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeFalse())
		})

		It("should re-apply overrides if they differ from manifest", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Cluster has different overrides than manifest
			cluster.InstallConfigOverrides = `{"fips": false}`

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster).NotTo(BeNil())
			Expect(updatedCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeTrue())
		})

		It("should return error if update fails", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Set transport to return error
			fakeTransport.SetUpdateError("API error")

			cluster.InstallConfigOverrides = ""

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("API error"))
			Expect(updatedCluster).To(BeNil())
		})

		It("should return error if get cluster fails after update", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Set transport to fail on get cluster
			fakeTransport.SetGetClusterError("Get cluster failed")

			cluster.InstallConfigOverrides = ""

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get cluster after applying installConfig overrides"))
			Expect(updatedCluster).To(BeNil())
		})
	})

	Context("when installConfig overrides are not present", func() {
		It("should return nil without error", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciNoOverride), 0600)
			Expect(err).NotTo(HaveOccurred())

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster).To(BeNil())
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeFalse())
		})
	})

	Context("when manifest file is invalid", func() {
		It("should return error if file does not exist", func() {
			fakeLogger, _ := test.NewNullLogger()
			invalidPath := filepath.Join(tempDir, "does-not-exist.yaml")

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, invalidPath)

			Expect(err).To(HaveOccurred())
			Expect(updatedCluster).To(BeNil())
		})

		It("should return error if file is not valid YAML", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte("invalid: yaml: content:"), 0600)
			Expect(err).NotTo(HaveOccurred())

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			Expect(err).To(HaveOccurred())
			Expect(updatedCluster).To(BeNil())
		})
	})

	Context("when existing cluster has invalid JSON", func() {
		It("should apply overrides when cluster has invalid JSON", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(aciFilePath, []byte(aciWithOveride), 0600)
			Expect(err).NotTo(HaveOccurred())

			// Cluster has invalid JSON in overrides
			cluster.InstallConfigOverrides = "{invalid json}"

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			// Should succeed and apply the valid overrides
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedCluster).NotTo(BeNil())
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeTrue())
			Expect(updatedCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
		})

		It("should return error if new overrides are invalid JSON", func() {
			fakeLogger, _ := test.NewNullLogger()

			// Create ACI with invalid JSON in overrides annotation
			invalidACI := `apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  annotations:
    agent-install.openshift.io/install-config-overrides: '{invalid json}'
  name: test-cluster
spec:
  clusterDeploymentRef:
    name: test-cluster
`
			err := os.WriteFile(aciFilePath, []byte(invalidACI), 0600)
			Expect(err).NotTo(HaveOccurred())

			updatedCluster, err := ApplyInstallConfigOverrides(ctx, fakeLogger, bmInventory, cluster, aciFilePath)

			// Should fail because new overrides are invalid
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to normalize new installConfig overrides"))
			Expect(updatedCluster).To(BeNil())
			Expect(fakeTransport.updateClusterInstallConfigCalled).To(BeFalse())
		})
	})
})

// mockInstallConfigTransport is a mock transport for testing ApplyInstallConfigOverrides
type mockInstallConfigTransport struct {
	updateClusterInstallConfigCalled bool
	lastInstallConfigParams          string
	updateError                      error
	getClusterError                  error
}

func NewMockInstallConfigTransport() *mockInstallConfigTransport {
	return &mockInstallConfigTransport{}
}

func (m *mockInstallConfigTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	switch v := op.Params.(type) {
	case *installerclient.V2UpdateClusterInstallConfigParams:
		if m.updateError != nil {
			return nil, m.updateError
		}
		m.updateClusterInstallConfigCalled = true
		m.lastInstallConfigParams = v.InstallConfigParams
		return &installerclient.V2UpdateClusterInstallConfigCreated{}, nil

	case *installerclient.V2GetClusterParams:
		if m.getClusterError != nil {
			return nil, m.getClusterError
		}
		return &installerclient.V2GetClusterOK{
			Payload: &models.Cluster{
				ID:                     &v.ClusterID,
				InstallConfigOverrides: m.lastInstallConfigParams,
			},
		}, nil

	default:
		return nil, fmt.Errorf("[mockInstallConfigTransport] unmanaged type: %T", v)
	}
}

func (m *mockInstallConfigTransport) SetUpdateError(errMsg string) {
	m.updateError = fmt.Errorf("%s", errMsg)
}

func (m *mockInstallConfigTransport) SetGetClusterError(errMsg string) {
	m.getClusterError = fmt.Errorf("%s", errMsg)
}

var _ = Describe("normalizeJSON", func() {
	It("should normalize JSON with different whitespace", func() {
		json1 := `{"fips":true,"networking":{"networkType":"OVNKubernetes"}}`
		json2 := `{
  "fips": true,
  "networking": {
    "networkType": "OVNKubernetes"
  }
}`
		normalized1, err1 := normalizeJSON(json1)
		normalized2, err2 := normalizeJSON(json2)

		Expect(err1).NotTo(HaveOccurred())
		Expect(err2).NotTo(HaveOccurred())
		Expect(normalized1).To(Equal(normalized2))
	})

	It("should normalize JSON with different key ordering", func() {
		json1 := `{"b":2,"a":1}`
		json2 := `{"a":1,"b":2}`

		normalized1, err1 := normalizeJSON(json1)
		normalized2, err2 := normalizeJSON(json2)

		Expect(err1).NotTo(HaveOccurred())
		Expect(err2).NotTo(HaveOccurred())
		Expect(normalized1).To(Equal(normalized2))
	})

	It("should handle empty string", func() {
		normalized, err := normalizeJSON("")

		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal(""))
	})

	It("should return error for invalid JSON", func() {
		_, err := normalizeJSON("{invalid json}")

		Expect(err).To(HaveOccurred())
	})

	It("should produce consistent output for identical content", func() {
		json1 := `{"fips":true}`

		normalized1, err1 := normalizeJSON(json1)
		normalized2, err2 := normalizeJSON(json1)

		Expect(err1).NotTo(HaveOccurred())
		Expect(err2).NotTo(HaveOccurred())
		Expect(normalized1).To(Equal(normalized2))
		Expect(normalized1).To(Equal(`{"fips":true}`))
	})
})

var _ = Describe("RegisterExtraManifests", func() {
	var (
		ctx                context.Context
		fakeManifestClient *mockManifestTransport
		manifestClient     *manifestsclient.Client
		cluster            *models.Cluster
		clusterID          strfmt.UUID
		manifestContent1   string
		manifestContent2   string
	)

	BeforeEach(func() {
		ctx = context.Background()

		clusterID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
		cluster = &models.Cluster{
			ID: &clusterID,
		}

		manifestContent1 = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test1\n"
		manifestContent2 = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test2\n"

		fakeManifestClient = NewMockManifestTransport()
		manifestClient = manifestsclient.New(fakeManifestClient, nil, nil)
	})

	Context("when no manifests exist", func() {
		It("should create all manifests", func() {
			fakeLogger, _ := test.NewNullLogger()
			fsys := fstest.MapFS{
				"manifest1.yml":  {Data: []byte(manifestContent1)},
				"manifest2.yaml": {Data: []byte(manifestContent2)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeManifestClient.createdManifests).To(HaveLen(2))
			Expect(fakeManifestClient.createdManifests).To(HaveKey("manifest1.yml"))
			Expect(fakeManifestClient.createdManifests).To(HaveKey("manifest2.yaml"))
		})

		It("should handle no manifests in directory", func() {
			fakeLogger, _ := test.NewNullLogger()
			fsys := fstest.MapFS{}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeManifestClient.createdManifests).To(HaveLen(0))
		})
	})

	Context("when manifests already exist", func() {
		It("should skip manifests with same content", func() {
			fakeLogger, _ := test.NewNullLogger()
			// Pre-populate existing manifests
			fakeManifestClient.existingManifests = map[string]string{
				"manifest1.yml": manifestContent1,
			}

			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
				"manifest2.yml": {Data: []byte(manifestContent2)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).NotTo(HaveOccurred())
			// Should only create manifest2, skipping manifest1
			Expect(fakeManifestClient.createdManifests).To(HaveLen(1))
			Expect(fakeManifestClient.createdManifests).To(HaveKey("manifest2.yml"))
			Expect(fakeManifestClient.createdManifests).NotTo(HaveKey("manifest1.yml"))
		})

		It("should return error if manifest exists with different content", func() {
			fakeLogger, _ := test.NewNullLogger()
			// Pre-populate existing manifest with different content
			fakeManifestClient.existingManifests = map[string]string{
				"manifest1.yml": "different content",
			}

			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already exists with different content"))
		})

		It("should be fully idempotent when called multiple times", func() {
			fakeLogger, _ := test.NewNullLogger()
			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
			}

			// First call - creates manifest
			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeManifestClient.createdManifests).To(HaveLen(1))

			// Simulate that the manifest now exists
			fakeManifestClient.existingManifests["manifest1.yml"] = manifestContent1

			// Second call - should skip creation
			err = RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			// Still only one manifest created (not duplicated)
			Expect(fakeManifestClient.createdManifests).To(HaveLen(1))
		})
	})

	Context("when API errors occur", func() {
		It("should return error if list manifests fails", func() {
			fakeLogger, _ := test.NewNullLogger()
			fakeManifestClient.SetListError("API error")

			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("API error"))
		})

		It("should return error if download manifest fails", func() {
			fakeLogger, _ := test.NewNullLogger()
			fakeManifestClient.existingManifests = map[string]string{
				"manifest1.yml": manifestContent1,
			}
			fakeManifestClient.SetDownloadError("Download failed")

			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Download failed"))
		})

		It("should return error if create manifest fails", func() {
			fakeLogger, _ := test.NewNullLogger()
			fakeManifestClient.SetCreateError("Create failed")

			fsys := fstest.MapFS{
				"manifest1.yml": {Data: []byte(manifestContent1)},
			}

			err := RegisterExtraManifests(fsys, ctx, fakeLogger, manifestClient, cluster)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Create failed"))
		})
	})
})

// mockManifestTransport is a mock transport for testing RegisterExtraManifests
type mockManifestTransport struct {
	existingManifests map[string]string // filename -> content
	createdManifests  map[string]string // filename -> content
	listError         error
	downloadError     error
	createError       error
}

func NewMockManifestTransport() *mockManifestTransport {
	return &mockManifestTransport{
		existingManifests: make(map[string]string),
		createdManifests:  make(map[string]string),
	}
}

// mockClientResponse implements runtime.ClientResponse for testing
type mockClientResponse struct {
	body io.ReadCloser
	code int
}

func (m *mockClientResponse) Code() int {
	return m.code
}

func (m *mockClientResponse) Message() string {
	return ""
}

func (m *mockClientResponse) GetHeader(string) string {
	return ""
}

func (m *mockClientResponse) GetHeaders(string) []string {
	return nil
}

func (m *mockClientResponse) Body() io.ReadCloser {
	return m.body
}

func (m *mockManifestTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	switch v := op.Params.(type) {
	case *manifestsclient.V2ListClusterManifestsParams:
		if m.listError != nil {
			return nil, m.listError
		}

		var manifestList []*models.Manifest
		for filename := range m.existingManifests {
			folder := "openshift"
			manifestList = append(manifestList, &models.Manifest{
				FileName: filename,
				Folder:   folder,
			})
		}

		return &manifestsclient.V2ListClusterManifestsOK{
			Payload: manifestList,
		}, nil

	case *manifestsclient.V2DownloadClusterManifestParams:
		if m.downloadError != nil {
			return nil, m.downloadError
		}

		filename := v.FileName
		content, exists := m.existingManifests[filename]
		if !exists {
			return nil, fmt.Errorf("manifest not found: %s", filename)
		}

		// Create a mock response that the Reader can process
		mockResponse := &mockClientResponse{
			body: io.NopCloser(bytes.NewBufferString(content)),
			code: 200,
		}

		// Call the Reader's ReadResponse method to write to the buffer
		// Use ByteStreamConsumer for plain text/YAML content (not JSON)
		return op.Reader.ReadResponse(mockResponse, runtime.ByteStreamConsumer())

	case *manifestsclient.V2CreateClusterManifestParams:
		if m.createError != nil {
			return nil, m.createError
		}

		filename := *v.CreateManifestParams.FileName
		encodedContent := *v.CreateManifestParams.Content
		decodedContent, err := base64.StdEncoding.DecodeString(encodedContent)
		if err != nil {
			return nil, err
		}

		m.createdManifests[filename] = string(decodedContent)

		return &manifestsclient.V2CreateClusterManifestCreated{
			Payload: &models.Manifest{
				FileName: filename,
				Folder:   *v.CreateManifestParams.Folder,
			},
		}, nil

	default:
		return nil, fmt.Errorf("[mockManifestTransport] unmanaged type: %T", v)
	}
}

func (m *mockManifestTransport) SetListError(errMsg string) {
	m.listError = fmt.Errorf("%s", errMsg)
}

func (m *mockManifestTransport) SetDownloadError(errMsg string) {
	m.downloadError = fmt.Errorf("%s", errMsg)
}

func (m *mockManifestTransport) SetCreateError(errMsg string) {
	m.createError = fmt.Errorf("%s", errMsg)
}

var _ = Describe("RegisterInfraEnv", func() {
	var (
		ctx                context.Context
		fakeTransport      *mockInfraEnvTransport
		bmInventory        *client.AssistedInstall
		pullSecret         string
		tempDir            string
		infraEnvFilePath   string
		nmStateConfigPath  string
		infraEnvManifest   string
		nmStateManifest    string
	)

	BeforeEach(func() {
		ctx = context.Background()

		pullSecret = `{"auths":{"cloud.openshift.com":{"auth":"test"}}}`

		fakeTransport = NewMockInfraEnvTransport()
		fakeInstallerClient := installerclient.New(fakeTransport, nil, nil)
		bmInventory = &client.AssistedInstall{
			Installer: fakeInstallerClient,
		}

		// Create temp directory for test manifests
		var err error
		tempDir, err = os.MkdirTemp("", "infraenv-test-*")
		Expect(err).NotTo(HaveOccurred())

		infraEnvFilePath = filepath.Join(tempDir, "infraenv.yaml")
		nmStateConfigPath = filepath.Join(tempDir, "nmstateconfig.yaml")

		// Minimal InfraEnv manifest
		infraEnvManifest = `apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: test-infraenv
  namespace: test-namespace
spec:
  clusterRef:
    name: test-cluster
    namespace: test-namespace
  pullSecretRef:
    name: pull-secret
  sshAuthorizedKey: ssh-rsa test-key
  nmStateConfigLabelSelector:
    matchLabels:
      infraenvs.agent-install.openshift.io: test-infraenv
`

		// NMStateConfig manifest
		nmStateManifest = `apiVersion: agent-install.openshift.io/v1beta1
kind: NMStateConfig
metadata:
  name: test-nmstate
  namespace: test-namespace
  labels:
    infraenvs.agent-install.openshift.io: test-infraenv
spec:
  interfaces:
    - name: eth0
      type: ethernet
      state: up
      ipv4:
        enabled: true
        address:
          - ip: 192.168.111.20
            prefix-length: 24
      mac-address: "02:00:00:80:12:14"
`

		err = os.WriteFile(infraEnvFilePath, []byte(infraEnvManifest), 0600)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("when NMStateConfig file exists", func() {
		It("should use the NMStateConfig for static network config", func() {
			fakeLogger, _ := test.NewNullLogger()
			err := os.WriteFile(nmStateConfigPath, []byte(nmStateManifest), 0600)
			Expect(err).NotTo(HaveOccurred())

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, nil,
				infraEnvFilePath, nmStateConfigPath, "full-iso", "")

			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv).NotTo(BeNil())

			// Verify the transport received the params with real NMStateConfig
			Expect(fakeTransport.lastInfraEnvParams).NotTo(BeNil())
			Expect(fakeTransport.lastInfraEnvParams.StaticNetworkConfig).To(HaveLen(1))

			// Should have eth0, not dummy0
			staticConfig := fakeTransport.lastInfraEnvParams.StaticNetworkConfig[0]
			Expect(staticConfig.NetworkYaml).To(ContainSubstring("eth0"))
			Expect(staticConfig.NetworkYaml).NotTo(ContainSubstring("dummy0"))
		})
	})

	Context("when NMStateConfig file does not exist", func() {
		It("should set placeholder StaticNetworkConfig with dummy0 interface", func() {
			fakeLogger, _ := test.NewNullLogger()

			// Don't create nmStateConfigPath file - it won't exist

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, nil,
				infraEnvFilePath, nmStateConfigPath, "full-iso", "")

			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv).NotTo(BeNil())

			// Verify the transport received params with placeholder
			Expect(fakeTransport.lastInfraEnvParams).NotTo(BeNil())
			Expect(fakeTransport.lastInfraEnvParams.StaticNetworkConfig).To(HaveLen(1))

			// Verify placeholder structure
			staticConfig := fakeTransport.lastInfraEnvParams.StaticNetworkConfig[0]
			Expect(staticConfig.NetworkYaml).To(ContainSubstring("dummy0"))
			Expect(staticConfig.NetworkYaml).To(ContainSubstring("type: dummy"))
			Expect(staticConfig.NetworkYaml).To(ContainSubstring("state: down"))
			Expect(staticConfig.MacInterfaceMap).To(HaveLen(1))
			Expect(staticConfig.MacInterfaceMap[0].LogicalNicName).To(Equal("dummy0"))
			Expect(staticConfig.MacInterfaceMap[0].MacAddress).To(Equal("02:00:00:00:00:00"))
		})
	})

	Context("when registering with a cluster reference", func() {
		It("should include cluster ID in params", func() {
			fakeLogger, _ := test.NewNullLogger()
			clusterID := strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
			cluster := &models.Cluster{
				ID: &clusterID,
			}

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, cluster,
				infraEnvFilePath, nmStateConfigPath, "full-iso", "")

			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv).NotTo(BeNil())
			Expect(infraEnv.ClusterID).To(Equal(clusterID))
		})
	})

	Context("when registering without a cluster reference", func() {
		It("should succeed for late binding scenario", func() {
			fakeLogger, _ := test.NewNullLogger()

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, nil,
				infraEnvFilePath, nmStateConfigPath, "full-iso", "")

			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv).NotTo(BeNil())
			// For late binding, ClusterID should be nil/empty
		})
	})

	Context("when API errors occur", func() {
		It("should return error if RegisterInfraEnv API call fails", func() {
			fakeLogger, _ := test.NewNullLogger()
			fakeTransport.SetRegisterError("API error")

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, nil,
				infraEnvFilePath, nmStateConfigPath, "full-iso", "")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("API error"))
			Expect(infraEnv).To(BeNil())
		})
	})

	Context("when InfraEnv manifest file is invalid", func() {
		It("should return error if file does not exist", func() {
			fakeLogger, _ := test.NewNullLogger()
			invalidPath := filepath.Join(tempDir, "does-not-exist.yaml")

			infraEnv, err := RegisterInfraEnv(ctx, fakeLogger, bmInventory, pullSecret, nil,
				invalidPath, nmStateConfigPath, "full-iso", "")

			Expect(err).To(HaveOccurred())
			Expect(infraEnv).To(BeNil())
		})
	})
})

// mockInfraEnvTransport is a mock transport for testing RegisterInfraEnv
type mockInfraEnvTransport struct {
	lastInfraEnvParams *models.InfraEnvCreateParams
	registerError      error
}

func NewMockInfraEnvTransport() *mockInfraEnvTransport {
	return &mockInfraEnvTransport{}
}

func (m *mockInfraEnvTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	switch v := op.Params.(type) {
	case *installerclient.RegisterInfraEnvParams:
		if m.registerError != nil {
			return nil, m.registerError
		}

		m.lastInfraEnvParams = v.InfraenvCreateParams

		// Create response with the same params
		infraEnvID := strfmt.UUID("00000000-0000-0000-0000-000000000000")
		var clusterID strfmt.UUID
		if v.InfraenvCreateParams.ClusterID != nil {
			clusterID = *v.InfraenvCreateParams.ClusterID
		}

		return &installerclient.RegisterInfraEnvCreated{
			Payload: &models.InfraEnv{
				ID:        &infraEnvID,
				ClusterID: clusterID,
				Name:      v.InfraenvCreateParams.Name,
			},
		}, nil

	default:
		return nil, fmt.Errorf("[mockInfraEnvTransport] unmanaged type: %T", v)
	}
}

func (m *mockInfraEnvTransport) SetRegisterError(errMsg string) {
	m.registerError = fmt.Errorf("%s", errMsg)
}
