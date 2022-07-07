package agentbasedinstaller

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
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

func TestAgentbasedinstaller(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agentbasedinstaller Suite")
}

type mockTransport struct {
	filesReceived map[string]string
	result        manifests.V2CreateClusterManifestCreated
	err           error
}

func NewMockTransport() *mockTransport {
	return &mockTransport{
		filesReceived: make(map[string]string),
		result:        manifests.V2CreateClusterManifestCreated{},
	}
}

func (m *mockTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {

	params, _ := op.Params.(*manifests.V2CreateClusterManifestParams)
	m.filesReceived[*params.CreateManifestParams.FileName] = *params.CreateManifestParams.Content

	if m.err != nil {
		return nil, m.err
	}

	return &m.result, nil
}

func (m *mockTransport) SetResult(res manifests.V2CreateClusterManifestCreated) {
	m.result = res
}

func (m *mockTransport) SetError(errMsg string) {
	m.err = errors.New(errMsg)
}

func (m *mockTransport) FilesReceived() map[string]string {
	return m.filesReceived
}
