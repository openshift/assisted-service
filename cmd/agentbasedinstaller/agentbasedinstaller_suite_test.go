package agentbasedinstaller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	installerclient "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus/hooks/test"
)

type registerCase struct {
	files       []string
	clientError string

	expectedFilesSent int
	expectedError     string
}

var _ = DescribeTable(
	"Register",
	func(tc registerCase) {
		fakeLogger, _ := test.NewNullLogger()

		clusterID := strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")
		fakeCluster := &models.Cluster{
			ID: &clusterID,
		}

		fakeFileSystem := fstest.MapFS{}
		for _, f := range tc.files {
			fakeFileSystem[f] = &fstest.MapFile{Data: []byte(f)}
		}

		fakeTransport := NewMockTransport()
		if tc.clientError != "" {
			fakeTransport.SetError(tc.clientError)
		}

		fakeClient := manifests.New(fakeTransport, nil, nil)

		err := RegisterExtraManifests(fakeFileSystem, context.TODO(), fakeLogger, fakeClient, fakeCluster)
		if tc.expectedError == "" {
			Expect(err).ShouldNot(HaveOccurred())

			received := fakeTransport.FilesReceived()
			Expect(len(received)).To(Equal(tc.expectedFilesSent))

			for fileName, fileContent := range received {

				found := false
				for _, f := range tc.files {
					if f == fileName {
						content, decodeErr := base64.StdEncoding.DecodeString(fileContent)
						Expect(decodeErr).ShouldNot(HaveOccurred())
						Expect(f).To(Equal(string(content)))
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())
			}
		} else {
			Expect(tc.expectedError).To(Equal(err.Error()))
		}
	},
	Entry("only-manifests", registerCase{
		files: []string{
			"config-map-1.yaml",
			"config-map-2.yaml",
		},
		expectedFilesSent: 2,
	}),
	Entry("get-only-yaml-files", registerCase{
		files: []string{
			"config-map-1.yaml",
			"unwanted.exe",
			"config-map-2.yml",
		},
		expectedFilesSent: 2,
	}),
	Entry("no-files", registerCase{
		files:             []string{},
		expectedFilesSent: 0,
	}),
	Entry("client-error", registerCase{
		files: []string{
			"config-map-1.yaml",
			"unwanted.exe",
			"config-map-2.yml",
		},
		clientError:       "client-error",
		expectedError:     "client-error",
		expectedFilesSent: 0,
	}),
)

type importCase struct {
	files                map[string]string
	expectedImportParams *models.ImportClusterParams
	expectedUpdateParams *models.V2ClusterUpdateParams
}

var clusterID = strfmt.UUID("e679ea3f-3b85-40e0-8dc9-82fd6945d9b2")

var _ = DescribeTable(
	"Import",
	func(tc importCase) {
		fakeLogger, _ := test.NewNullLogger()

		fakeFileSystem := fstest.MapFS{}
		for name, data := range tc.files {
			fakeFileSystem[name] = &fstest.MapFile{Data: []byte(data)}
		}

		fakeTransport := NewMockTransport()
		fakeInstallerClient := installerclient.New(fakeTransport, nil, nil)
		fakeBMClient := &client.AssistedInstall{
			Installer: fakeInstallerClient,
		}

		_, err := ImportCluster(fakeFileSystem, context.Background(), fakeLogger, fakeBMClient, clusterID, "ostest", "api.ostest")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeTransport.lastImportParamsReceived).To(BeEquivalentTo(tc.expectedImportParams))
		Expect(fakeTransport.lastUpdateParamsReceived).To(BeEquivalentTo(tc.expectedUpdateParams))
	},
	Entry("default", importCase{
		files: map[string]string{
			"import-cluster-config.json": "{\"networking\": {\"userManagedNetworking\": true}}",
		},
		expectedImportParams: &models.ImportClusterParams{
			APIVipDnsname:      swag.String("api.ostest"),
			Name:               swag.String("ostest"),
			OpenshiftClusterID: &clusterID,
		},
		expectedUpdateParams: &models.V2ClusterUpdateParams{
			UserManagedNetworking: swag.Bool(true),
		},
	}),
	Entry("(optional) ignition endpoint config", importCase{
		files: map[string]string{
			"import-cluster-config.json":    "{\"networking\": {\"userManagedNetworking\": false}}",
			"worker-ignition-endpoint.json": "{\"url\": \"https://192.168.111.5:22623/config/worker\", \"ca_certificate\": \"LS0tL_FakeCertificate_LS0tCg==\"}",
		},
		expectedImportParams: &models.ImportClusterParams{
			APIVipDnsname:      swag.String("api.ostest"),
			Name:               swag.String("ostest"),
			OpenshiftClusterID: &clusterID,
		},
		expectedUpdateParams: &models.V2ClusterUpdateParams{
			IgnitionEndpoint: &models.IgnitionEndpoint{
				URL:           swag.String("https://192.168.111.5:22623/config/worker"),
				CaCertificate: swag.String("LS0tL_FakeCertificate_LS0tCg=="),
			},
			UserManagedNetworking: swag.Bool(false),
		},
	}),
)

func TestAgentbasedinstaller(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agentbasedinstaller Suite")
}

type mockTransport struct {
	filesReceived            map[string]string
	lastImportParamsReceived *models.ImportClusterParams
	lastUpdateParamsReceived *models.V2ClusterUpdateParams
	err                      error
}

func NewMockTransport() *mockTransport {
	return &mockTransport{
		filesReceived: make(map[string]string),
	}
}

func (m *mockTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	var result interface{}

	switch v := op.Params.(type) {
	case *manifests.V2CreateClusterManifestParams:
		m.filesReceived[*v.CreateManifestParams.FileName] = *v.CreateManifestParams.Content
		result = &manifests.V2CreateClusterManifestCreated{}
	case *manifests.V2ListClusterManifestsParams:
		result = &manifests.V2ListClusterManifestsOK{
			Payload: []*models.Manifest{},
		}
	case *installerclient.V2ImportClusterParams:
		m.lastImportParamsReceived = v.NewImportClusterParams
		result = &installerclient.V2ImportClusterCreated{
			Payload: &models.Cluster{
				ID: v.NewImportClusterParams.OpenshiftClusterID,
			},
		}
	case *installerclient.V2UpdateClusterParams:
		m.lastUpdateParamsReceived = v.ClusterUpdateParams
		result = &installerclient.V2UpdateClusterCreated{}
	default:
		return nil, fmt.Errorf("[mockTransport] unmanaged type: %T", v)
	}

	if m.err != nil {
		return nil, m.err
	}

	return result, nil
}

func (m *mockTransport) SetError(errMsg string) {
	m.err = errors.New(errMsg)
}

func (m *mockTransport) FilesReceived() map[string]string {
	return m.filesReceived
}
