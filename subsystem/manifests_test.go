package subsystem

import (
	"bytes"
	"context"
	"encoding/base64"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("manifests tests", func() {
	var (
		ctx           = context.Background()
		cluster       *models.Cluster
		content       = "hello world!"
		base64Content = base64.RawStdEncoding.EncodeToString([]byte(content))
		manifestFile  models.Manifest
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		manifestFile = models.Manifest{
			FileName: "99-test.yaml",
			Folder:   "openshift",
		}

		var err error
		cluster, err = userBMClient.API.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("[minimal-set]upload_download_manifest", func() {
		var originalFilesAmount int

		By("List files before upload", func() {
			response, err := userBMClient.Manifests.ListClusterManifests(ctx, &manifests.ListClusterManifestsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			originalFilesAmount = len(response.Payload)
		})

		By("upload", func() {
			response, err := userBMClient.Manifests.CreateClusterManifest(ctx, &manifests.CreateClusterManifestParams{
				ClusterID: *cluster.ID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &base64Content,
					FileName: &manifestFile.FileName,
					Folder:   &manifestFile.Folder,
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*response.Payload).Should(Equal(manifestFile))
		})

		By("List files after upload", func() {
			response, err := userBMClient.Manifests.ListClusterManifests(ctx, &manifests.ListClusterManifestsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(response.Payload).Should(HaveLen(originalFilesAmount + 1))

			var found bool = false
			for _, manifest := range response.Payload {
				if *manifest == manifestFile {
					found = true
					break
				}
			}

			Expect(found).Should(BeTrue())
		})

		By("download", func() {
			buffer := new(bytes.Buffer)

			_, err := userBMClient.Manifests.DownloadClusterManifest(ctx, &manifests.DownloadClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  manifestFile.FileName,
				Folder:    &manifestFile.Folder,
			}, buffer)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(buffer.String()).Should(Equal(content))
		})

		By("delete", func() {
			_, err := userBMClient.Manifests.DeleteClusterManifest(ctx, &manifests.DeleteClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  manifestFile.FileName,
				Folder:    &manifestFile.Folder,
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		By("List files after delete", func() {
			response, err := userBMClient.Manifests.ListClusterManifests(ctx, &manifests.ListClusterManifestsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(response.Payload).Should(HaveLen(originalFilesAmount))

			var found bool = false
			for _, manifest := range response.Payload {
				if *manifest == manifestFile {
					found = true
					break
				}
			}

			Expect(found).Should(BeFalse())
		})
	})
})
