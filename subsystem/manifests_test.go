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
		ctx     = context.Background()
		cluster *models.Cluster
		content = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 99-openshift-machineconfig-master-kargs
spec:
  kernelArguments:
    - 'loglevel=7'`
		base64Content = base64.StdEncoding.EncodeToString([]byte(content))
		manifestFile  models.Manifest
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		manifestFile = models.Manifest{
			FileName: "99-openshift-machineconfig-master-kargs.yaml",
			Folder:   "openshift",
		}

		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
				BaseDNSDomain:    "example.com",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
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

	It("check installation telemeter manifests", func() {

		isProdDeployment := func() bool {
			return Options.InventoryHost != "api.stage.openshift.com" && Options.InventoryHost != "api.integration.openshift.com"
		}

		if isProdDeployment() {
			Skip("No manifest is generated for prod cloud deployment")
		}

		clusterID := *cluster.ID

		By("install cluster", func() {
			registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
			reply, err := userBMClient.Installer.InstallCluster(context.Background(), &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := reply.GetPayload()
			Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
			generateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)
			waitForInstallationPreparationCompletionStatus(clusterID, common.InstallationPreparationSucceeded)
		})

		By("list manifests", func() {
			response, err := userBMClient.Manifests.ListClusterManifests(ctx, &manifests.ListClusterManifestsParams{
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			found := false
			for _, manifest := range response.Payload {
				if manifest.FileName == "redirect-telemeter.yaml" && manifest.Folder == models.ManifestFolderOpenshift {
					found = true
				}
			}
			Expect(found).To(BeTrue())
		})
	})
})
