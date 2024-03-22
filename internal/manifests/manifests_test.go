package manifests_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"gorm.io/gorm"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)

	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "manifests_test")
}

const contentAsYAMLPatch = `---
- op: replace
  path: /status/infrastructureTopology
  value: HighlyAvailable`

const contentAsYAML = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
  machineconfiguration.openshift.io/role: master
  name: 99-openshift-machineconfig-master-kargs
spec:
  kernelArguments:
  - 'loglevel=7'`

const contentAsJSON = `{
  "apiVersion": "machineconfiguration.openshift.io/v1",
  "kind": "MachineConfig",
  "metadata": null,
  "labels": null,
  "machineconfiguration.openshift.io/role": "master",
  "name": "99-openshift-machineconfig-master-kargs",
  "spec": null,
  "kernelArguments": [
    "loglevel=7"
  ]
}`

var _ = Describe("ClusterManifestTests", func() {
	var (
		manifestsAPI      *manifests.Manifests
		db                *gorm.DB
		ctx               = context.Background()
		ctrl              *gomock.Controller
		mockS3Client      *s3wrapper.MockAPI
		dbName            string
		fileNameYaml      = "99-openshift-machineconfig-master-kargs.yaml"
		fileNameYamlPatch = "99-openshift-machineconfig-master-kargs.yaml.patch.foobar"
		fileNameYmlPatch  = "99-openshift-machineconfig-master-kargs.yml.patch.foobar"
		fileNameJson      = "99-openshift-machineconfig-master-kargs.json"
		validFolder       = "openshift"
		defaultFolder     = "manifests"
		contentYaml       = encodeToBase64(contentAsYAML)
		contentYamlPatch  = encodeToBase64(contentAsYAMLPatch)
		contentJson       = encodeToBase64(contentAsJSON)
		mockUsageAPI      *usage.MockAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockUsageAPI = usage.NewMockAPI(ctrl)
		manifestsAPI = manifests.NewManifestsAPI(db, common.GetTestLog(), mockS3Client, mockUsageAPI)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	registerCluster := func() *common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:     &clusterID,
				Status: swag.String(models.ClusterStatusReady),
			},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		return &cluster
	}

	expectUsageCalls := func() {
		mockUsageAPI.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockUsageAPI.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	}
	addManifestToCluster := func(clusterID *strfmt.UUID, content, fileName, folderName string) {
		expectUsageCalls()
		response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
			ClusterID: *clusterID,
			CreateManifestParams: &models.CreateManifestParams{
				Content:  &content,
				FileName: &fileName,
				Folder:   &folderName,
			},
		})
		Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
	}

	getObjectName := func(clusterID *strfmt.UUID, folderName, fileName string) string {
		return fmt.Sprintf("%s/manifests/%s/%s", *clusterID, folderName, fileName)
	}

	getMetadataObjectName := func(clusterID *strfmt.UUID, folderName, fileName string, manifestSource string) string {
		return fmt.Sprintf("%s/manifest-attributes/%s/%s/%s", *clusterID, folderName, fileName, manifestSource)
	}

	mockObjectExists := func(exists bool) {
		mockS3Client.EXPECT().DoesObjectExist(ctx, gomock.Any()).Return(exists, nil).AnyTimes()
	}

	mockUpload := func(times int) {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}

	mockDownloadFailure := func() {
		mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(nil, int64(0), errors.New("Simulated download failure")).MinTimes(0)
	}

	mockListByPrefix := func(clusterID *strfmt.UUID, files []string) {
		prefix := fmt.Sprintf("%s/manifests", *clusterID)
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
	}

	Context("CreateClusterManifest", func() {
		It("creates manifest successfully with default folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			expectUsageCalls()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileNameYaml)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &contentYaml,
					FileName: &fileNameYaml,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload := response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("creates manifest successfully with 'openshift' folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			expectUsageCalls()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", fileNameYaml)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &contentYaml,
					FileName: &fileNameYaml,
					Folder:   &validFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload := response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(validFolder))
		})

		It("override an existing manifest", func() {
			clusterID := registerCluster().ID
			mockUpload(2)
			expectUsageCalls()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileNameYaml)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &contentYaml,
					FileName: &fileNameYaml,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload := response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))

			expectUsageCalls()
			response = manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &contentYaml,
					FileName: &fileNameYaml,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload = response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &contentYaml,
					FileName: &fileNameYaml,
				},
			})

			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})

		It("fails due to non-base64 file content", func() {
			clusterID := registerCluster().ID
			invalidContent := "not base64 content"
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileNameYaml)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &invalidContent,
					FileName: &fileNameYaml,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			Expect(err.Error()).To(ContainSubstring("failed to base64-decode cluster manifest content"))
		})

		Context("File validation and format", func() {

			generateLargeJSON := func(length int) string {
				var largeElementBuilder strings.Builder
				largeElementBuilder.Grow(length)
				for i := 0; i <= length; i++ {
					largeElementBuilder.WriteString("A")
				}
				return fmt.Sprintf("{\"data\":\"%s\"}", largeElementBuilder.String())
			}

			It("Does not accept a filename that does not contain a name before the extension", func() {
				clusterID := registerCluster().ID
				content := "{}"
				fileName := ".yaml"
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileName,
					},
				})
				err := response.(*common.ApiErrorResponse)
				expectedErrorMessage := fmt.Sprintf("Cluster manifest %s for cluster %s has an invalid filename.", fileName, clusterID)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusUnprocessableEntity)))
				Expect(err.Error()).To(Equal(expectedErrorMessage))
			})

			It("Does not accept a filename that contains spaces", func() {
				clusterID := registerCluster().ID
				content := "{}"
				fileName := "dest FileName"
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileName,
					},
				})
				err := response.(*common.ApiErrorResponse)
				expectedErrorMessage := fmt.Sprintf("Cluster manifest %s for cluster %s should not include a space in its name.", fileName, clusterID)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusUnprocessableEntity)))
				Expect(err.Error()).To(Equal(expectedErrorMessage))
			})

			It("accepts manifest in json format and .json extension", func() {
				clusterID := registerCluster().ID
				jsonContent := encodeToBase64(contentAsJSON)
				fileName := "99-openshift-machineconfig-master-kargs.json"
				mockUpload(1)
				expectUsageCalls()
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &jsonContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
				responsePayload := response.(*operations.V2CreateClusterManifestCreated)
				Expect(responsePayload.Payload).ShouldNot(BeNil())
				Expect(responsePayload.Payload.FileName).To(Equal(fileName))
				Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
			})

			It("accepts manifest with .yml extension", func() {
				clusterID := registerCluster().ID
				fileName := "99-openshift-machineconfig-master-kargs.yaml"
				mockUpload(1)
				expectUsageCalls()
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileNameYaml)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileNameYaml,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
				responsePayload := response.(*operations.V2CreateClusterManifestCreated)
				Expect(responsePayload.Payload).ShouldNot(BeNil())
				Expect(responsePayload.Payload.FileName).To(Equal(fileName))
				Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
			})

			It("accepts manifest with .yml extension and a multi YAML content", func() {
				clusterID := registerCluster().ID
				fileName := "99_masters-chrony-configuration.yaml"
				aContent := encodeToBase64(`---
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: masters-chrony-configuration
spec:
  config:
    ignition:
      config: {}
      security:
        tls: {}
      timeouts: {}
      version: 2.2.0
    networkd: {}
    passwd: {}
    storage:
      files:
      - contents:
          source: data:text/plain;charset=utf-8;base64,c2VydmVyIGNsb2NrLnJlZGhhdC5jb20gaWJ1cnN0Cg==
          verification: {}
        filesystem: root
        mode: 420
        path: /etc/chrony.conf
  osImageURL: ""
---
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: masters-chrony-configuration
spec:
  config:
    ignition:
      config: {}
      security:
        tls: {}
      timeouts: {}
      version: 2.2.0
`)
				mockUpload(1)
				expectUsageCalls()
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &aContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
				responsePayload := response.(*operations.V2CreateClusterManifestCreated)
				Expect(responsePayload.Payload).ShouldNot(BeNil())
				Expect(responsePayload.Payload.FileName).To(Equal(fileName))
				Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
			})

			It("fails for manifest with invalid json format", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.json"
				invalidJSONContent := encodeToBase64("not a valid JSON content")
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &invalidJSONContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest content of file manifests/99-test.json for cluster ID " + clusterID.String() + " has an illegal JSON format"))
			})

			It("fails for manifest with invalid yaml format", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.yml"
				invalidYAMLContent := encodeToBase64("invalid YAML content: {")
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &invalidYAMLContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(Equal("Manifest content of file manifests/99-test.yml for cluster ID " + clusterID.String() + " has an invalid YAML format: yaml: line 1: did not find expected node content"))
			})

			It("fails for yaml patch with invalid patch format", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.yml.patch"
				invalidPatchContent := contentYaml
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &invalidPatchContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Patch content of file manifests/99-test.yml.patch for cluster ID " + clusterID.String() + " is invalid:"))
			})

			It("fails for manifest with unsupported extension", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.txt"
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest filename of file manifests/99-test.txt for cluster ID " + clusterID.String() + " is invalid. Only json, yaml and yml or patch extensions are supported"))
			})

			It("manifest creation does not fail for yaml patch files", func() {
				mockUpload(1)
				expectUsageCalls()
				clusterID := registerCluster().ID
				fileName := fileNameYamlPatch
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYamlPatch,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			})

			It("manifest creation does not fail for yml patch files", func() {
				mockUpload(1)
				expectUsageCalls()
				clusterID := registerCluster().ID
				fileName := fileNameYmlPatch
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYamlPatch,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			})

			It("fails for filename that contains folder in the name", func() {
				clusterID := registerCluster().ID
				fileNameWithFolder := "openshift/99-test.yaml"
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileNameWithFolder,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusUnprocessableEntity)))
				Expect(err.Error()).To(ContainSubstring("Cluster manifest openshift/99-test.yaml for cluster " + clusterID.String() + " should not include a directory in it's name."))
			})

			It("Creation fails for a manifest file that exceeds the maximum upload size", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.json"
				maxFileSizeBytes := 1024*1024 + 1
				largeJSONContent := encodeToBase64(generateLargeJSON(maxFileSizeBytes))
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileName)).Return(false, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &largeJSONContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest content of file manifests/99-test.json for cluster ID " + clusterID.String() + " exceeds the maximum file size of 1MiB"))
			})

			It("Update fails for a manifest file that exceeds the maximum upload size", func() {
				clusterID := registerCluster().ID
				maxFileSizeBytes := 1024*1024 + 1
				largeJSONContent := encodeToBase64(generateLargeJSON(maxFileSizeBytes))
				response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
					ClusterID: *clusterID,
					UpdateManifestParams: &models.UpdateManifestParams{
						UpdatedContent: &largeJSONContent,
						FileName:       fileNameJson,
						Folder:         defaultFolder,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest content of file 99-openshift-machineconfig-master-kargs.json for cluster ID " + clusterID.String() + " exceeds the maximum file size of 1MiB"))
			})

			It("should reject upload of a file to manifests folder if it already exists in openshift folder", func() {
				clusterID := registerCluster().ID
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", fileNameYaml)).Return(true, nil).AnyTimes()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileNameYaml,
					},
				})
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				expectedError := fmt.Sprintf("manifest file %s for cluster ID %s in folder %s cannot be uploaded as it is not distinct between {manifest, openshift} folders", fileNameYaml, clusterID.String(), "manifests")
				Expect(err.Error()).To(ContainSubstring(expectedError))
			})

			It("should reject upload of a file to openshift folder if it already exists in manifests folder", func() {
				clusterID := registerCluster().ID
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", fileNameYaml)).Return(true, nil).AnyTimes()
				uploadFolder := "openshift"
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileNameYaml,
						Folder:   &uploadFolder,
					},
				})
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				expectedError := fmt.Sprintf("manifest file %s for cluster ID %s in folder %s cannot be uploaded as it is not distinct between {manifest, openshift} folders", fileNameYaml, clusterID.String(), "openshift")
				Expect(err.Error()).To(ContainSubstring(expectedError))
			})

			It("should reject upload of a file to a folder that is not openshift/manifests", func() {
				clusterID := registerCluster().ID
				uploadFolder := "somefolder"
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &contentYaml,
						FileName: &fileNameYaml,
						Folder:   &uploadFolder,
					},
				})
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				expectedError := fmt.Sprintf("Supplied folder (%s) in cluster %s should be one of {openshift, manifests}", uploadFolder, clusterID.String())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			})

			It("Upload succeeds when each yaml in multi-doc yaml file is valid", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.yaml"
				content := encodeToBase64(`---
first: one
---
---
- second: two
`)
				mockUpload(1)
				mockObjectExists(false)
				expectUsageCalls()
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
				responsePayload := response.(*operations.V2CreateClusterManifestCreated)
				Expect(responsePayload.Payload).ShouldNot(BeNil())
				Expect(responsePayload.Payload.FileName).To(Equal(fileName))
				Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
			})

			It("Upload fails when one of the yaml in muti-doc yaml file is invalid", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.yml"
				content := encodeToBase64(`---
first: one
---
invalid YAML content: {
`)
				response := manifestsAPI.V2CreateClusterManifest(ctx, operations.V2CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(Equal("Manifest content of file manifests/99-test.yml for cluster ID " + clusterID.String() + " has an invalid YAML format: yaml: line 4: did not find expected node content"))
			})
		})
	})

	Context("V2ListClusterManifests", func() {

		It("should not filter system manifests if requested to show all", func() {
			manifests := []models.Manifest{
				{
					FileName: "file-1.yaml",
					Folder:   validFolder,
				},
				{
					FileName: "file-2.yaml",
					Folder:   defaultFolder,
				},
				{
					FileName: "file-3.yaml",
					Folder:   defaultFolder,
				},
			}

			clusterID := registerCluster().ID
			files := make([]string, 0)

			mockUpload(len(manifests))

			manifestMetadataPrefix := filepath.Join(clusterID.String(), "manifest-attributes")
			for _, file := range manifests {
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", file.FileName)).Return(false, nil).AnyTimes()
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", file.FileName)).Return(false, nil).AnyTimes()
				files = append(files, getObjectName(clusterID, file.Folder, file.FileName))
				addManifestToCluster(clusterID, contentYaml, file.FileName, file.Folder)
				if file.FileName == "file-2.yaml" {
					mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(manifestMetadataPrefix, file.Folder, file.FileName, constants.ManifestSourceUserSupplied)).Return(false, nil).Times(1)
				} else {
					mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(manifestMetadataPrefix, file.Folder, file.FileName, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
				}
			}
			mockListByPrefix(clusterID, files)

			includeSystemGenerated := true
			response := manifestsAPI.V2ListClusterManifests(ctx, operations.V2ListClusterManifestsParams{
				ClusterID:              *clusterID,
				IncludeSystemGenerated: &includeSystemGenerated,
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2ListClusterManifestsOK()))
			responsePayload := response.(*operations.V2ListClusterManifestsOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(len(responsePayload.Payload)).To(Equal(len(manifests)))

			for i := range manifests {
				Expect(manifests).To(ContainElement(*responsePayload.Payload[i]))
			}
		})

		It("lists manifest from different folders", func() {
			manifests := []models.Manifest{
				{
					FileName: "file-1.yaml",
					Folder:   validFolder,
				},
				{
					FileName: "file-3.yaml",
					Folder:   defaultFolder,
				},
			}

			clusterID := registerCluster().ID
			files := make([]string, 0)

			mockUpload(len(manifests))

			manifestMetadataPrefix := filepath.Join(clusterID.String(), "manifest-attributes")
			for _, file := range manifests {
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", file.FileName)).Return(false, nil).AnyTimes()
				mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", file.FileName)).Return(false, nil).AnyTimes()
				files = append(files, getObjectName(clusterID, file.Folder, file.FileName))
				addManifestToCluster(clusterID, contentYaml, file.FileName, file.Folder)
				if file.FileName == "file-2.yaml" {
					mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(manifestMetadataPrefix, file.Folder, file.FileName, constants.ManifestSourceUserSupplied)).Return(false, nil).Times(1)
				} else {
					mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(manifestMetadataPrefix, file.Folder, file.FileName, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
				}
			}
			mockListByPrefix(clusterID, files)

			response := manifestsAPI.V2ListClusterManifests(ctx, operations.V2ListClusterManifestsParams{
				ClusterID: *clusterID,
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2ListClusterManifestsOK()))
			responsePayload := response.(*operations.V2ListClusterManifestsOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(len(responsePayload.Payload)).To(Equal(len(manifests)))

			for i := range manifests {
				Expect(manifests).To(ContainElement(*responsePayload.Payload[i]))
			}
		})

		It("list manifests for new cluster", func() {
			clusterID := registerCluster().ID
			mockListByPrefix(clusterID, []string{})
			response := manifestsAPI.V2ListClusterManifests(ctx, operations.V2ListClusterManifestsParams{
				ClusterID: *clusterID,
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2ListClusterManifestsOK()))
			responsePayload := response.(*operations.V2ListClusterManifestsOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(len(responsePayload.Payload)).To(Equal(0))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.V2ListClusterManifests(ctx, operations.V2ListClusterManifestsParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
			})

			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})
	})

	Context("V2DeleteClusterManifest", func() {
		It("deletes manifest from default folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, defaultFolder, "file-1.yaml")).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, "file-1.yaml")).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, defaultFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockListByPrefix(clusterID, []string{})
			mockUsageAPI.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(1)
			mockUsageAPI.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "file-1.yaml")).Return(false, nil).AnyTimes()
			addManifestToCluster(clusterID, contentYaml, "file-1.yaml", defaultFolder)
			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("deletes one of two manifests", func() {
			clusterID := registerCluster().ID
			mockUpload(2)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, defaultFolder, "file-1.yaml")).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, "file-1.yaml")).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, defaultFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockListByPrefix(clusterID, []string{"file-2.yaml"})
			mockUsageAPI.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)
			mockUsageAPI.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "file-1.yaml")).Return(false, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "file-2.yaml")).Return(false, nil).AnyTimes()
			addManifestToCluster(clusterID, contentYaml, "file-1.yaml", defaultFolder)
			addManifestToCluster(clusterID, contentYaml, "file-2.yaml", defaultFolder)

			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("deletes manifest from a different folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, validFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, validFolder, "file-1.yaml")).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, validFolder, "file-1.yaml")).Return(true, nil)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, validFolder, "file-1.yaml", constants.ManifestSourceUserSupplied)).Return(true, nil)
			mockListByPrefix(clusterID, []string{})
			mockUsageAPI.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(1)
			mockUsageAPI.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", "file-1.yaml")).Return(false, nil).AnyTimes()
			addManifestToCluster(clusterID, contentYaml, "file-1.yaml", validFolder)

			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
				Folder:    &validFolder,
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("deletes missing manifest", func() {
			clusterID := registerCluster().ID
			mockObjectExists(false)
			mockListByPrefix(clusterID, []string{})
			mockUsageAPI.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(1)
			mockUsageAPI.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)

			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
				FileName:  "file-1.yaml",
			})

			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})

		It("deletes after installation has been started", func() {
			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:     &clusterID,
					Status: swag.String(models.ClusterStatusInstalling),
				},
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			response := manifestsAPI.V2DeleteClusterManifest(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
		})
	})

	Context("DownloadClusterManifest", func() {
		It("downloads manifest from different folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().Download(ctx, gomock.Any()).Return(VoidReadCloser{}, int64(0), nil)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", "file-1.yaml")).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "file-1.yaml")).Return(false, nil).AnyTimes()
			addManifestToCluster(clusterID, contentYaml, "file-1.yaml", defaultFolder)

			response := manifestsAPI.V2DownloadClusterManifest(ctx, operations.V2DownloadClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0), nil)))
		})

		It("downloads missing manifest", func() {
			clusterID := registerCluster().ID
			mockObjectExists(false)

			response := manifestsAPI.V2DownloadClusterManifest(ctx, operations.V2DownloadClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.V2DownloadClusterManifest(ctx, operations.V2DownloadClusterManifestParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
			})

			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})
	})

	Context("UpdateClusterManifest", func() {
		It("Does not accept a filename that contains spaces", func() {
			clusterID := registerCluster().ID
			destFolder := "manifests"
			destFileName := "dest FileName"
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        fileNameYaml,
					Folder:          defaultFolder,
					UpdatedFolder:   &destFolder,
					UpdatedFileName: &destFileName,
				},
			})
			err := response.(*common.ApiErrorResponse)
			expectedErrorMessage := fmt.Sprintf("Cluster manifest %s for cluster %s should not include a space in its name.", destFileName, clusterID)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusUnprocessableEntity)))
			Expect(err.Error()).To(Equal(expectedErrorMessage))
		})

		It("moves existing file from one path to another", func() {
			clusterID := registerCluster().ID
			destFolder := "manifests"
			destFileName := "destFileName"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, destFolder, destFileName)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, destFolder, destFileName, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", destFileName)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        fileNameYaml,
					Folder:          defaultFolder,
					UpdatedFolder:   &destFolder,
					UpdatedFileName: &destFileName,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(destFileName))
			Expect(responsePayload.Payload.Folder).To(Equal(destFolder))
		})

		It("emits error when download failure encountered during file move", func() {
			clusterID := registerCluster().ID
			destFolder := "manifests"
			destFileName := "destFileName"
			mockDownloadFailure()
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", destFileName)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        fileNameYaml,
					Folder:          defaultFolder,
					UpdatedFolder:   &destFolder,
					UpdatedFileName: &destFileName,
				},
			})
			err := response.(*common.ApiErrorResponse)
			expectedErrorMessage := fmt.Sprintf("Failed to fetch content from %s for cluster %s: Simulated download failure", filepath.Join(defaultFolder, fileNameYaml), clusterID)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusInternalServerError)))
			Expect(err.Error()).To(Equal(expectedErrorMessage))
		})

		It("updates existing file with new content if content is correct for yaml", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					UpdatedContent: &contentYaml,
					FileName:       fileNameYaml,
					Folder:         defaultFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("updates existing file with new content if content is correct for yaml patch", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYamlPatch, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					UpdatedContent: &contentYamlPatch,
					FileName:       fileNameYamlPatch,
					Folder:         defaultFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYamlPatch))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("updates existing file with new content if content is correct for yml patch", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYmlPatch, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					UpdatedContent: &contentYamlPatch,
					FileName:       fileNameYmlPatch,
					Folder:         defaultFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYmlPatch))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("updates existing file with new content if content is correct for json", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameJson, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					UpdatedContent: &contentJson,
					FileName:       fileNameJson,
					Folder:         defaultFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameJson))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("returns an error if content is incorrect for json", func() {
			clusterID := registerCluster().ID
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					UpdatedContent: &contentYaml,
					FileName:       fileNameJson,
					Folder:         defaultFolder,
				},
			})
			err := response.(*common.ApiErrorResponse)
			expectedError := fmt.Sprintf("Manifest content of file %s for cluster ID %s has an illegal JSON format", fileNameJson, clusterID)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			Expect(err.Error()).To(Equal(expectedError))
		})

		It("Should use the source file name when the updated file name is omitted ", func() {
			clusterID := registerCluster().ID
			destFolder := "openshift"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, destFolder, fileNameYaml)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, destFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:      fileNameYaml,
					Folder:        defaultFolder,
					UpdatedFolder: &destFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileNameYaml))
			Expect(responsePayload.Payload.Folder).To(Equal(destFolder))
		})

		It("Should use the source folder when updated folder is omitted", func() {
			clusterID := registerCluster().ID
			destFileName := "destFileName.yaml"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, defaultFolder, destFileName)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, defaultFolder, destFileName, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, fileNameYaml)).Return(true, nil)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, defaultFolder, fileNameYaml, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", destFileName)).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        fileNameYaml,
					Folder:          defaultFolder,
					UpdatedFileName: &destFileName,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
			responsePayload := response.(*operations.V2UpdateClusterManifestOK)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(destFileName))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("should reject an update where folders and files have changed and desitination file would exist in manifests and openshift", func() {
			clusterID := registerCluster().ID
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "test2.json")).Return(true, nil).AnyTimes()
			srcFolder := "openshift"
			srcFileName := "test.json"
			destFolder := "manifests"
			destFileName := "test2.json"
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        srcFileName,
					Folder:          srcFolder,
					UpdatedFileName: &destFileName,
					UpdatedFolder:   &destFolder,
				},
			})
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			expectedError := fmt.Sprintf("manifest file %s for cluster ID %s in folder %s cannot be uploaded as it is not distinct between {manifest, openshift} folders", destFileName, clusterID.String(), destFolder)
			Expect(err.Error()).To(ContainSubstring(expectedError))
		})

		It("should accept an update where folders and files have changed and desitination file would not exist in manifests and openshift", func() {
			clusterID := registerCluster().ID
			srcFolder := "openshift"
			srcFileName := "test.json"
			destFolder := "manifests"
			destFileName := "test2.json"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, destFolder, destFileName)).Return(nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, destFolder, destFileName, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, srcFolder, srcFileName, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(true, nil)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, srcFolder, srcFileName, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "test2.json")).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        srcFileName,
					Folder:          srcFolder,
					UpdatedFileName: &destFileName,
					UpdatedFolder:   &destFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
		})

		It("should reject an update where folders remain the same and files have changed and desitination file would exist in manifests and openshift", func() {
			clusterID := registerCluster().ID
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", "test2.json")).Return(true, nil).AnyTimes()
			srcFolder := "openshift"
			srcFileName := "test.json"
			destFolder := "openshift"
			destFileName := "test2.json"
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        srcFileName,
					Folder:          srcFolder,
					UpdatedFileName: &destFileName,
					UpdatedFolder:   &destFolder,
				},
			})
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			expectedError := fmt.Sprintf("manifest file %s for cluster ID %s in folder %s cannot be uploaded as it is not distinct between {manifest, openshift} folders", destFileName, clusterID.String(), destFolder)
			Expect(err.Error()).To(ContainSubstring(expectedError))
		})

		It("should accept an update where folders remain the same and files have changed and desitination file would not exist in manifests and openshift", func() {
			clusterID := registerCluster().ID
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", "test2.json")).Return(false, nil).AnyTimes()
			srcFolder := "openshift"
			srcFileName := "test.json"
			destFolder := "openshift"
			destFileName := "test2.json"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, destFolder, destFileName)).Return(nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, destFolder, destFileName, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, srcFolder, srcFileName, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(true, nil)
			mockS3Client.EXPECT().DeleteObject(ctx, getMetadataObjectName(clusterID, srcFolder, srcFileName, constants.ManifestSourceUserSupplied)).Return(true, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "openshift", "test2.json")).Return(false, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        srcFileName,
					Folder:          srcFolder,
					UpdatedFileName: &destFileName,
					UpdatedFolder:   &destFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
		})

		It("should accept an update where folders remain the same and files remain the same", func() {
			clusterID := registerCluster().ID
			mockS3Client.EXPECT().DoesObjectExist(ctx, filepath.Join(clusterID.String(), constants.ManifestFolder, "manifests", "test2.json")).Return(false, nil).AnyTimes()
			srcFolder := "openshift"
			srcFileName := "test.json"
			destFolder := "openshift"
			destFileName := "test.json"
			reader := io.NopCloser(strings.NewReader(contentAsYAML))
			mockS3Client.EXPECT().Download(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(reader, int64(0), nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte(contentAsYAML), getObjectName(clusterID, destFolder, destFileName)).Return(nil).Times(1)
			mockS3Client.EXPECT().Upload(ctx, []byte{}, getMetadataObjectName(clusterID, destFolder, destFileName, constants.ManifestSourceUserSupplied)).Return(nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(ctx, getMetadataObjectName(clusterID, srcFolder, srcFileName, constants.ManifestSourceUserSupplied)).Return(true, nil).AnyTimes()
			mockS3Client.EXPECT().DoesObjectExist(ctx, getObjectName(clusterID, srcFolder, srcFileName)).Return(true, nil).AnyTimes()
			response := manifestsAPI.V2UpdateClusterManifest(ctx, operations.V2UpdateClusterManifestParams{
				ClusterID: *clusterID,
				UpdateManifestParams: &models.UpdateManifestParams{
					FileName:        srcFileName,
					Folder:          srcFolder,
					UpdatedFileName: &destFileName,
					UpdatedFolder:   &destFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2UpdateClusterManifestOK()))
		})
	})

	Describe("ParsePath", func() {
		It("Should parse a full manifest path into folder and filename", func() {
			folder, filename, err := manifests.ParsePath("2716d677-8052-463e-be12-d45e3aa05db0/manifests/openshift/file-1.yaml")
			Expect(err).Should(BeNil())
			Expect(folder).Should(Equal("openshift"))
			Expect(filename).Should(Equal("file-1.yaml"))
		})

		It("Should return an error if supplied a non-manifest path", func() {
			folder, filename, err := manifests.ParsePath("2716d677-8052-463e-be12-d45e3aa05db0/something.ign")
			Expect(err.Error()).Should(Equal("Filepath 2716d677-8052-463e-be12-d45e3aa05db0/something.ign is not a manifest path"))
			Expect(folder).Should(Equal(""))
			Expect(filename).Should(Equal(""))
		})
	})

	Describe("IsManifest", func() {
		It("Should be able to determine if a given manifest path is a manifest path", func() {
			Expect(manifests.IsManifest("2716d677-8052-463e-be12-d45e3aa05db0/manifests/openshift/file-1.yaml")).To(BeTrue())
		})

		It("Should be able to determine if a given manifest path is not a manifest path", func() {
			Expect(manifests.IsManifest("2716d677-8052-463e-be12-d45e3aa05db0/something.ign")).To(BeFalse())
		})
	})

	Describe("IsUserManifest", func() {
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockS3Client = s3wrapper.NewMockAPI(ctrl)
			mockUsageAPI = usage.NewMockAPI(ctrl)
			manifestsAPI = manifests.NewManifestsAPI(db, common.GetTestLog(), mockS3Client, mockUsageAPI)
		})

		It("Should determine that a manifest is user generated if there is a metadata file for it", func() {
			clusterId := strfmt.UUID(uuid.New().String())
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"user-defined-manifest.yaml", "user-supplied")).Return(true, nil).Times(1)
			Expect(manifestsAPI.IsUserManifest(ctx, clusterId, "openshift", "user-defined-manifest.yaml")).To(BeTrue())
		})

		It("Should determine that a manifest is not user generated if there is no metadata file for it", func() {
			clusterId := strfmt.UUID(uuid.New().String())
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"system-generated-manifest.yaml", "user-supplied")).Return(false, nil).Times(1)
			Expect(manifestsAPI.IsUserManifest(ctx, clusterId, "openshift", "system-generated-manifest.yaml")).To(BeFalse())
		})
	})

	Describe("ListClusterManifestsInternal", func() {

		It("Should list both system generated and user manifests if IncludeSystemGenerated is true", func() {
			clusterId := registerCluster().ID
			manifests := []string{
				filepath.Join(clusterId.String(), constants.ManifestFolder, "openshift", "system-generated-manifest.yaml"),
				filepath.Join(clusterId.String(), constants.ManifestFolder, "openshift", "user-generated-manifest.yaml"),
			}
			objectName := filepath.Join(clusterId.String(), constants.ManifestFolder)
			mockS3Client.EXPECT().ListObjectsByPrefix(ctx, objectName).Return(manifests, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"system-generated-manifest.yaml", "user-supplied")).Return(false, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"user-generated-manifest.yaml", "user-supplied")).Return(true, nil).Times(1)
			includeSystemGenerated := true
			listedManifests, err := manifestsAPI.ListClusterManifestsInternal(ctx, operations.V2ListClusterManifestsParams{
				ClusterID:              *clusterId,
				IncludeSystemGenerated: &includeSystemGenerated,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(listedManifests)).To(Equal(2))
			Expect(listedManifests[0].Folder).To(Equal("openshift"))
			Expect(listedManifests[0].FileName).To(Equal("system-generated-manifest.yaml"))
			Expect(listedManifests[1].Folder).To(Equal("openshift"))
			Expect(listedManifests[1].FileName).To(Equal("user-generated-manifest.yaml"))
		})

		It("Should be able to list only user manifests if user and non user manifests are present", func() {
			clusterId := registerCluster().ID
			manifests := []string{
				filepath.Join(clusterId.String(), constants.ManifestFolder, "openshift", "system-generated-manifest.yaml"),
				filepath.Join(clusterId.String(), constants.ManifestFolder, "openshift", "user-generated-manifest.yaml"),
			}
			objectName := filepath.Join(clusterId.String(), constants.ManifestFolder)
			mockS3Client.EXPECT().ListObjectsByPrefix(ctx, objectName).Return(manifests, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"system-generated-manifest.yaml", "user-supplied")).Return(false, nil).Times(1)
			mockS3Client.EXPECT().DoesObjectExist(
				ctx,
				filepath.Join(
					clusterId.String(),
					constants.ManifestMetadataFolder,
					"openshift",
					"user-generated-manifest.yaml", "user-supplied")).Return(true, nil).Times(1)
			includeSystemGenerated := false
			listedManifests, err := manifestsAPI.ListClusterManifestsInternal(ctx, operations.V2ListClusterManifestsParams{
				ClusterID:              *clusterId,
				IncludeSystemGenerated: &includeSystemGenerated,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(listedManifests)).To(Equal(1))
			Expect(listedManifests[0].Folder).To(Equal("openshift"))
			Expect(listedManifests[0].FileName).To(Equal("user-generated-manifest.yaml"))
		})
	})

	Context("GetManifestMetadata", func() {
		It("Should be able to list manifest metadata", func() {
			clusterId := registerCluster().ID
			s3Metadata := []string{
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "first.yaml", constants.ManifestSourceUserSupplied),
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "openshift", "second.yaml", constants.ManifestSourceUserSupplied),
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "third.yaml", "some-other-manifest-source"),
			}
			mockS3Client.EXPECT().ListObjectsByPrefix(ctx, filepath.Join(clusterId.String(), constants.ManifestMetadataFolder)).Return(s3Metadata, nil).Times(1)
			userManifests, err := manifests.GetManifestMetadata(ctx, clusterId, mockS3Client)
			Expect(err).NotTo(HaveOccurred())
			Expect(userManifests).To(ConsistOf(s3Metadata))
		})
	})
	Context("FilterMetadataOnManifestSource", func() {
		It("returns metadata paths when matching manifest source", func() {
			clusterId := registerCluster().ID
			firstMetadata := filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "first.yaml", constants.ManifestSourceUserSupplied)
			secondMetadata := filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "openshift", "second.yaml", constants.ManifestSourceUserSupplied)
			s3Metadata := []string{
				firstMetadata,
				secondMetadata,
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "third.yaml", "some-other-manifest-source"),
			}
			filteredMetadata := manifests.FilterMetadataOnManifestSource(s3Metadata, constants.ManifestSourceUserSupplied)
			Expect(filteredMetadata).To(ConsistOf(firstMetadata, secondMetadata))
		})
		It("returns an empty list when no metadata have matched the manifest seource", func() {
			clusterId := registerCluster().ID
			s3Metadata := []string{
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "first.yaml", constants.ManifestSourceUserSupplied),
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "openshift", "second.yaml", constants.ManifestSourceUserSupplied),
			}
			filteredMetadata := manifests.FilterMetadataOnManifestSource(s3Metadata, "some-other-manifest-source")
			Expect(filteredMetadata).To(ConsistOf())
		})
		It("returns an empty list when metadata are malformed", func() {
			s3Metadata := []string{
				"first.yaml",
				"",
			}
			filteredMetadata := manifests.FilterMetadataOnManifestSource(s3Metadata, "some-other-manifest-source")
			Expect(filteredMetadata).To(ConsistOf())
		})
	})
	Context("ResolveManifestNamesFromMetadata", func() {
		It("returns manifest names when metadata paths are valid", func() {
			clusterId := registerCluster().ID
			s3Metadata := []string{
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "first.yaml", constants.ManifestSourceUserSupplied),
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "openshift", "second.yaml", constants.ManifestSourceUserSupplied),
				filepath.Join(clusterId.String(), constants.ManifestMetadataFolder, "manifests", "third.yaml", "some-other-manifest-source"),
			}
			manifestNames, err := manifests.ResolveManifestNamesFromMetadata(s3Metadata)
			Expect(err).NotTo(HaveOccurred())
			Expect(manifestNames).To(ConsistOf("manifests/first.yaml", "openshift/second.yaml", "manifests/third.yaml"))
		})

		for _, invalidMetadata := range []string{"foo.yaml", "foo/bar", ""} {
			invalidMetadata_ := invalidMetadata
			It("returns an error when metadata are malformed", func() {
				_, err := manifests.ResolveManifestNamesFromMetadata([]string{invalidMetadata_})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(
					fmt.Sprintf("Failed to extract manifest name from metadata path %s", invalidMetadata_),
				))
			})
		}

	})
})

type VoidReadCloser struct {
}

func (VoidReadCloser) Read(p []byte) (int, error) {
	return 0, nil
}

func (VoidReadCloser) Close() error {
	return nil
}

func encodeToBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
