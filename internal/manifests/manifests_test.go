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
		manifestsAPI  *manifests.Manifests
		db            *gorm.DB
		ctx           = context.Background()
		ctrl          *gomock.Controller
		mockS3Client  *s3wrapper.MockAPI
		dbName        string
		fileNameYaml  = "99-openshift-machineconfig-master-kargs.yaml"
		fileNameJson  = "99-openshift-machineconfig-master-kargs.json"
		validFolder   = "openshift"
		defaultFolder = "manifests"
		contentYaml   = encodeToBase64(contentAsYAML)
		contentJson   = encodeToBase64(contentAsJSON)
		mockUsageAPI  *usage.MockAPI
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

			It("accepts manifest in json format and .json extension", func() {
				clusterID := registerCluster().ID
				jsonContent := encodeToBase64(contentAsJSON)
				fileName := "99-openshift-machineconfig-master-kargs.json"
				mockUpload(1)
				expectUsageCalls()
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
				invalidYAMLContent := encodeToBase64("not a valid YAML content")
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
				Expect(err.Error()).To(ContainSubstring("Manifest content of file manifests/99-test.yml for cluster ID " + clusterID.String() + " has an invalid YAML format"))
			})

			It("fails for manifest with unsupported extension", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.txt"
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
				Expect(err.Error()).To(ContainSubstring("Manifest filename of file manifests/99-test.txt for cluster ID " + clusterID.String() + " is invalid. Only json, yaml and yml extensions are supported"))
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
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Cluster manifest openshift/99-test.yaml for cluster " + clusterID.String() + " should not include a directory in its name."))
			})

			It("Creation fails for a manifest file that exceeds the maximum upload size", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.json"
				maxFileSizeBytes := 1024*1024 + 1
				largeJSONContent := encodeToBase64(generateLargeJSON(maxFileSizeBytes))
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

		})
	})

	Context("V2ListClusterManifests", func() {
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
			addManifestToCluster(clusterID, contentYaml, "file-1.yaml", defaultFolder)
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
			mockObjectExists(true)
			mockS3Client.EXPECT().Download(ctx, gomock.Any()).Return(VoidReadCloser{}, int64(0), nil)
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
