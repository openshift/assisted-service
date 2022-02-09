package manifests_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
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
		fileName      = "99-openshift-machineconfig-master-kargs.yaml"
		validFolder   = "openshift"
		defaultFolder = "manifests"
		content       = encodeToBase64(contentAsYAML)
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
		response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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

	mockObjectExists := func(exists bool) {
		mockS3Client.EXPECT().DoesObjectExist(ctx, gomock.Any()).Return(exists, nil).Times(1)
	}

	mockUpload := func(times int) {
		mockS3Client.EXPECT().Upload(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(times)
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
			response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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

		It("creates manifest successfully with 'openshift' folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			expectUsageCalls()
			response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &content,
					FileName: &fileName,
					Folder:   &validFolder,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload := response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileName))
			Expect(responsePayload.Payload.Folder).To(Equal(validFolder))
		})

		It("override an existing manifest", func() {
			clusterID := registerCluster().ID
			mockUpload(2)
			expectUsageCalls()
			response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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

			expectUsageCalls()
			response = manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &content,
					FileName: &fileName,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2CreateClusterManifestCreated()))
			responsePayload = response.(*operations.V2CreateClusterManifestCreated)
			Expect(responsePayload.Payload).ShouldNot(BeNil())
			Expect(responsePayload.Payload.FileName).To(Equal(fileName))
			Expect(responsePayload.Payload.Folder).To(Equal(defaultFolder))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &content,
					FileName: &fileName,
				},
			})

			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})

		It("fails due to non-base64 file content", func() {
			clusterID := registerCluster().ID
			invalidContent := "not base64 content"
			response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
				ClusterID: *clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &invalidContent,
					FileName: &fileName,
				},
			})
			Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
			err := response.(*common.ApiErrorResponse)
			Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			Expect(err.Error()).To(ContainSubstring("failed to base64-decode cluster manifest content"))
		})

		Context("File validation and format", func() {
			It("accepts manifest in json format and .json extension", func() {
				clusterID := registerCluster().ID
				jsonContent := encodeToBase64(contentAsJSON)
				fileName := "99-openshift-machineconfig-master-kargs.json"
				mockUpload(1)
				expectUsageCalls()
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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
				fileName := "99-openshift-machineconfig-master-kargs.yml"
				mockUpload(1)
				expectUsageCalls()
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
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
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &invalidJSONContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest content has an illegal JSON format"))
			})

			It("fails for manifest with invalid yaml format", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.yml"
				invalidYAMLContent := encodeToBase64("not a valid YAML content")
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &invalidYAMLContent,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Manifest content has an invalid YAML format"))
			})

			It("fails for manifest with unsupported extension", func() {
				clusterID := registerCluster().ID
				fileName := "99-test.txt"
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileName,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("Unsupported manifest extension"))
			})

			It("fails for filename that contains folder in the name", func() {
				clusterID := registerCluster().ID
				fileNameWithFolder := "openshift/99-test.yaml"
				response := manifestsAPI.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
					ClusterID: *clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &content,
						FileName: &fileNameWithFolder,
					},
				})
				Expect(response).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.New(""))))
				err := response.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(err.Error()).To(ContainSubstring("should not include a directory in its name"))
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
					FileName: "file-2.yaml",
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

			for _, file := range manifests {
				files = append(files, getObjectName(clusterID, file.Folder, file.FileName))
				addManifestToCluster(clusterID, content, file.FileName, file.Folder)
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

	Context("DeleteClusterManifest", func() {
		It("deletes manifest from default folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockObjectExists(true)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, defaultFolder, "file-1.yaml")).Return(true, nil)
			addManifestToCluster(clusterID, content, "file-1.yaml", defaultFolder)

			response := manifestsAPI.DeleteClusterManifest(ctx, operations.DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("deletes manifest from a different folder", func() {
			clusterID := registerCluster().ID
			mockUpload(1)
			mockObjectExists(true)
			mockS3Client.EXPECT().DeleteObject(ctx, getObjectName(clusterID, validFolder, "file-1.yaml")).Return(true, nil)
			addManifestToCluster(clusterID, content, "file-1.yaml", validFolder)

			response := manifestsAPI.DeleteClusterManifest(ctx, operations.DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
				Folder:    &validFolder,
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("deletes missing manifest", func() {
			clusterID := registerCluster().ID
			mockObjectExists(false)

			response := manifestsAPI.DeleteClusterManifest(ctx, operations.DeleteClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(operations.NewV2DeleteClusterManifestOK()))
		})

		It("cluster doesn't exist", func() {
			response := manifestsAPI.DeleteClusterManifest(ctx, operations.DeleteClusterManifestParams{
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
			response := manifestsAPI.DeleteClusterManifest(ctx, operations.DeleteClusterManifestParams{
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
			addManifestToCluster(clusterID, content, "file-1.yaml", defaultFolder)

			response := manifestsAPI.V2DownloadClusterManifest(ctx, operations.V2DownloadClusterManifestParams{
				ClusterID: *clusterID,
				FileName:  "file-1.yaml",
			})
			Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0))))
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
