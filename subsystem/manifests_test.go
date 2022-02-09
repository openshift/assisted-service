package subsystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"html/template"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/usage"
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
			response, err := userBMClient.Manifests.V2ListClusterManifests(ctx, &manifests.V2ListClusterManifestsParams{
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
			verifyUsage(true, *cluster.ID)
		})

		By("List files after upload", func() {
			response, err := userBMClient.Manifests.V2ListClusterManifests(ctx, &manifests.V2ListClusterManifestsParams{
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

			_, err := userBMClient.Manifests.V2DownloadClusterManifest(ctx, &manifests.V2DownloadClusterManifestParams{
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
			response, err := userBMClient.Manifests.V2ListClusterManifests(ctx, &manifests.V2ListClusterManifestsParams{
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
			response, err := userBMClient.Manifests.V2ListClusterManifests(ctx, &manifests.V2ListClusterManifestsParams{
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
			verifyUsage(true, clusterID)
		})
	})
})

var _ = Describe("disk encryption", func() {

	var (
		ctx                = context.Background()
		defaultTangServers = `[{"url":"http://tang.example.com:7500","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"},` +
			`{"URL":"http://tang.example.com:7501","Thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu8"}]`
	)

	It("test API", func() {

		var clusterID strfmt.UUID

		By("cluster creation", func() {

			registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					BaseDNSDomain:    "example.com",
					DiskEncryption: &models.DiskEncryption{
						EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
						Mode:     swag.String(models.DiskEncryptionModeTpmv2),
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			c := registerClusterReply.GetPayload()
			Expect(*c.DiskEncryption.EnableOn).To(Equal(models.DiskEncryptionEnableOnAll))
			Expect(*c.DiskEncryption.Mode).To(Equal(models.DiskEncryptionModeTpmv2))

			clusterID = *c.ID
		})

		By("cluster update", func() {

			updateClusterReply, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					DiskEncryption: &models.DiskEncryption{
						EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
						Mode:        swag.String(models.DiskEncryptionModeTang),
						TangServers: defaultTangServers,
					},
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())

			c := updateClusterReply.GetPayload()
			Expect(*c.DiskEncryption.EnableOn).To(Equal(models.DiskEncryptionEnableOnMasters))
			Expect(*c.DiskEncryption.Mode).To(Equal(models.DiskEncryptionModeTang))
			Expect(c.DiskEncryption.TangServers).To(Equal(defaultTangServers))
		})
	})

	Context("manifests generation", func() {

		const (
			tpmv2Template = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: {{.ROLE}}-tpm
  labels:
    machineconfiguration.openshift.io/role: {{.ROLE}}
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      luks:
        - name: root
          device: /dev/disk/by-partlabel/root
          clevis:
            tpm2: true
          options: [--cipher, aes-cbc-essiv:sha256]
          wipeVolume: true
      filesystems:
        - device: /dev/mapper/root
          format: xfs
          wipeFilesystem: true
          label: root`

			tangTemplate = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: {{.ROLE}}-tang
  labels:
    machineconfiguration.openshift.io/role: {{.ROLE}}
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      luks:
        - name: root
          device: /dev/disk/by-partlabel/root
          clevis:
            tang:
              - url: http://tang.example.com:7500
                thumbprint: PLjNyRdGw03zlRoGjQYMahSZGu9
              - url: http://tang.example.com:7501
                thumbprint: PLjNyRdGw03zlRoGjQYMahSZGu8
          options: [--cipher, aes-cbc-essiv:sha256]
          wipeVolume: true
      filesystems:
        - device: /dev/mapper/root
          format: xfs
          wipeFilesystem: true
          label: root
  kernelArguments:
    - rd.neednet=1`
		)

		var (
			tpmv2MasterManifest = &bytes.Buffer{}
			tpmv2WorkerManifest = &bytes.Buffer{}
			tangMasterManifest  = &bytes.Buffer{}
			tangWorkerManifest  = &bytes.Buffer{}
			openshiftFolder     = "openshift"
		)

		tmpl, err := template.New("template").Parse(tpmv2Template)
		Expect(err).NotTo(HaveOccurred())

		err = tmpl.Execute(tpmv2MasterManifest, map[string]string{"ROLE": "master"})
		Expect(err).NotTo(HaveOccurred())

		err = tmpl.Execute(tpmv2WorkerManifest, map[string]string{"ROLE": "worker"})
		Expect(err).NotTo(HaveOccurred())

		tmpl, err = template.New("template").Parse(tangTemplate)
		Expect(err).NotTo(HaveOccurred())

		err = tmpl.Execute(tangMasterManifest, map[string]string{"ROLE": "master"})
		Expect(err).NotTo(HaveOccurred())

		err = tmpl.Execute(tangWorkerManifest, map[string]string{"ROLE": "worker"})
		Expect(err).NotTo(HaveOccurred())

		for _, t := range []struct {
			name                   string
			diskEncryption         *models.DiskEncryption
			expectedManifestsNames []string
			expectedManifests      []*bytes.Buffer
			reverseManifestsSearch bool
		}{
			{
				name: "all nodes, tpm2",
				diskEncryption: &models.DiskEncryption{
					EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
					Mode:     swag.String(models.DiskEncryptionModeTpmv2),
				},
				expectedManifestsNames: []string{
					"99-openshift-master-tpm-encryption.yaml",
					"99-openshift-worker-tpm-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tpmv2MasterManifest,
					tpmv2WorkerManifest,
				},
			},
			{
				name: "all nodes, tang",
				diskEncryption: &models.DiskEncryption{
					EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
					Mode:        swag.String(models.DiskEncryptionModeTang),
					TangServers: defaultTangServers,
				},
				expectedManifestsNames: []string{
					"99-openshift-master-tang-encryption.yaml",
					"99-openshift-worker-tang-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tangMasterManifest,
					tangWorkerManifest,
				},
			},
			{
				name: "masters only, tpmv2",
				diskEncryption: &models.DiskEncryption{
					EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
					Mode:     swag.String(models.DiskEncryptionModeTpmv2),
				},
				expectedManifestsNames: []string{
					"99-openshift-master-tpm-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tpmv2MasterManifest,
				},
			},
			{
				name: "masters only, tang",
				diskEncryption: &models.DiskEncryption{
					EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
					Mode:        swag.String(models.DiskEncryptionModeTang),
					TangServers: defaultTangServers,
				},
				expectedManifestsNames: []string{
					"99-openshift-master-tang-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tangMasterManifest,
				},
			},
			{
				name: "workers only, tpmv2",
				diskEncryption: &models.DiskEncryption{
					EnableOn: swag.String(models.DiskEncryptionEnableOnWorkers),
					Mode:     swag.String(models.DiskEncryptionModeTpmv2),
				},
				expectedManifestsNames: []string{
					"99-openshift-worker-tpm-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tpmv2WorkerManifest,
				},
			},
			{
				name: "workers only, tang",
				diskEncryption: &models.DiskEncryption{
					EnableOn:    swag.String(models.DiskEncryptionEnableOnWorkers),
					Mode:        swag.String(models.DiskEncryptionModeTang),
					TangServers: defaultTangServers,
				},
				expectedManifestsNames: []string{
					"99-openshift-worker-tang-encryption.yaml",
				},
				expectedManifests: []*bytes.Buffer{
					tangWorkerManifest,
				},
			},
			{
				name: "disk encryption not set",
				expectedManifestsNames: []string{
					"99-openshift-master-tpm-encryption.yaml",
					"99-openshift-worker-tpm-encryption.yaml",
					"99-openshift-master-tang-encryption.yaml",
					"99-openshift-worker-tang-encryption.yaml",
				},
				reverseManifestsSearch: true,
			},
		} {
			t := t

			It(t.name, func() {

				var clusterID strfmt.UUID

				By("register cluster", func() {

					registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							Name:             swag.String("test-cluster"),
							OpenshiftVersion: swag.String(openshiftVersion),
							PullSecret:       swag.String(pullSecret),
							SSHPublicKey:     sshPublicKey,
							BaseDNSDomain:    "example.com",
							DiskEncryption:   t.diskEncryption,
						},
					})
					Expect(err).NotTo(HaveOccurred())
					clusterID = *registerClusterReply.GetPayload().ID
				})

				By("install cluster", func() {

					generateClusterISO(clusterID, models.ImageTypeMinimalIso)
					registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
					reply, err := userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					c := reply.GetPayload()
					generateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)
					waitForInstallationPreparationCompletionStatus(clusterID, common.InstallationPreparationSucceeded)
				})

				By("verify manifests", func() {

					for i, manifestName := range t.expectedManifestsNames {

						manifest := &bytes.Buffer{}
						_, err := userBMClient.Manifests.V2DownloadClusterManifest(ctx, &manifests.V2DownloadClusterManifestParams{
							ClusterID: clusterID,
							FileName:  manifestName,
							Folder:    &openshiftFolder,
						}, manifest)

						if t.reverseManifestsSearch {
							Expect(err).To(HaveOccurred())
						} else {
							Expect(err).NotTo(HaveOccurred())
							Expect(manifest.String()).To(Equal(t.expectedManifests[i].String()))
						}
					}
				})
			})
		}
	})
})

func verifyUsage(set bool, clusterID strfmt.UUID) {
	getReply, err := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(clusterID))
	Expect(err).ToNot(HaveOccurred())
	c := &common.Cluster{Cluster: *getReply.Payload}
	if set {
		verifyUsageSet(c.FeatureUsage, models.Usage{Name: usage.CustomManifest})
	} else {
		verifyUsageNotSet(c.FeatureUsage, usage.CustomManifest)
	}
}
