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

var _ = Describe("ApplyInstallConfigOverrides", func() {
	var (
		ctx            context.Context
		logger         *test.Hook
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
		logger = logger // Use logger
		_ = fakeLogger  // Use fakeLogger
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
