package subsystem

import (
	"context"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("Disconnected Cluster for OVE", func() {
	ctx := context.Background()
	var (
		clusterID  strfmt.UUID
		infraEnv   *models.InfraEnv
		infraEnvID strfmt.UUID
	)

	createBasicCluster := func(name string) strfmt.UUID {
		cluster, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String(name),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				BaseDNSDomain:    "example.com",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		return *cluster.GetPayload().ID
	}

	Context("Temporary cluster for OVE ISO configuration", func() {
		BeforeEach(func() {
			utils_test.TestContext.DeregisterResources()
		})

		AfterEach(func() {
			utils_test.TestContext.DeregisterResources()
		})

		It("should transition cluster to disconnected status when disconnected-iso infraenv is registered", func() {
			By("Creating a cluster in insufficient status")
			clusterID = createBasicCluster("ove-temp-cluster")

			By("Verifying cluster is in insufficient state")
			clusterResp, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(clusterResp.GetPayload().Status)).Should(Equal(models.ClusterStatusInsufficient))

			By("Registering an InfraEnv with disconnected-iso type")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("ove-infraenv"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeDisconnectedIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()
			infraEnvID = *infraEnv.ID

			By("Verifying cluster status changed to disconnected")
			Eventually(func() string {
				resp, getErr := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				Expect(getErr).NotTo(HaveOccurred())
				return swag.StringValue(resp.GetPayload().Status)
			}, 10*time.Second, 1*time.Second).Should(Equal(models.ClusterStatusDisconnected))

			By("Verifying cluster status info is set correctly")
			clusterResp, err = utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(clusterResp.GetPayload().StatusInfo)).Should(Equal("Cluster is used for disconnected ISO configuration"))
		})

		It("should not allow host registration for disconnected-iso infraenv", func() {
			By("Creating a cluster with 0 hosts")
			clusterID = createBasicCluster("ove-temp-cluster-2")

			By("Registering a disconnected-iso InfraEnv")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("ove-infraenv-2"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeDisconnectedIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()
			infraEnvID = *infraEnv.ID

			By("Waiting for cluster to become disconnected")
			Eventually(func() string {
				resp, cluster_err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				Expect(cluster_err).NotTo(HaveOccurred())
				return swag.StringValue(resp.GetPayload().Status)
			}, 10*time.Second, 1*time.Second).Should(Equal(models.ClusterStatusDisconnected))

			By("Attempting to register a host to the disconnected-iso infraenv")
			hostID := strfmt.UUID(uuid.New().String())
			_, err = utils_test.TestContext.AgentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
				InfraEnvID: infraEnvID,
				NewHostParams: &models.HostCreateParams{
					HostID: &hostID,
				},
			})

			By("Verifying host registration is blocked for disconnected-iso infraenv")
			Expect(err).To(HaveOccurred())

			e := err.(*installer.V2RegisterHostBadRequest)
			Expect(*e.Payload.Reason).To(ContainSubstring("Cannot register a host to an InfraEnv with disconnected-iso type"))

			By("Verifying cluster remains in disconnected state")
			clusterResp, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(clusterResp.GetPayload().Status)).Should(Equal(models.ClusterStatusDisconnected))
		})

		It("should not set disconnected status for non-disconnected-iso infraenv types", func() {
			By("Creating a cluster with 0 hosts")
			clusterID = createBasicCluster("regular-cluster")

			By("Registering a regular full-iso InfraEnv")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("regular-infraenv"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeFullIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()

			By("Verifying cluster does not transition to disconnected state")
			Consistently(func() string {
				resp, cluster_err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				Expect(cluster_err).NotTo(HaveOccurred())
				return swag.StringValue(resp.GetPayload().Status)
			}, 5*time.Second, 1*time.Second).Should(Not(Equal(models.ClusterStatusDisconnected)))

			By("Verifying cluster is in either pending-for-input or insufficient state")
			clusterResp, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			status := swag.StringValue(clusterResp.GetPayload().Status)
			Expect(status).Should(Or(Equal(models.ClusterStatusPendingForInput), Equal(models.ClusterStatusInsufficient)))
		})

		It("should transition cluster from pending-for-input to disconnected status when disconnected-iso infraenv is registered", func() {
			By("Creating a cluster in insufficient status")
			cluster, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:              swag.String("pending-input-cluster"),
					OpenshiftVersion:  swag.String(openshiftVersion),
					PullSecret:        swag.String(pullSecret),
					BaseDNSDomain:     "example.com",
					VipDhcpAllocation: swag.Bool(false),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			clusterID = *cluster.GetPayload().ID

			By("Waiting for cluster monitoring to transition to pending-for-input state")
			waitForClusterState(ctx, clusterID, models.ClusterStatusPendingForInput, 60*time.Second,
				utils_test.ClusterPendingForInputStateInfo)

			By("Registering an InfraEnv with disconnected-iso type")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("ove-infraenv-pending"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeDisconnectedIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()
			infraEnvID = *infraEnv.ID

			By("Verifying cluster status changed to disconnected")
			Eventually(func() string {
				resp, getErr := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				Expect(getErr).NotTo(HaveOccurred())
				return swag.StringValue(resp.GetPayload().Status)
			}, 10*time.Second, 1*time.Second).Should(Equal(models.ClusterStatusDisconnected))

			By("Verifying cluster status info is set correctly")
			clusterResp, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(clusterResp.GetPayload().StatusInfo)).Should(Equal("Cluster is used for disconnected ISO configuration"))
		})

		It("should only update insufficient clusters to disconnected status", func() {
			By("Creating a cluster with 3 hosts to get it to ready state")
			cluster, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:              swag.String("ready-cluster"),
					OpenshiftVersion:  swag.String(openshiftVersion),
					PullSecret:        swag.String(pullSecret),
					BaseDNSDomain:     "example.com",
					ControlPlaneCount: swag.Int64(3),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			clusterID = *cluster.GetPayload().ID

			By("Registering a regular InfraEnv first to add hosts")
			regularInfraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("regular-infraenv-for-hosts"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeFullIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			regularInfraEnvID := *regularInfraEnvResp.GetPayload().ID

			By("Adding hosts to move cluster to ready state")
			c := cluster.GetPayload()
			hosts := registerHostsAndSetRoles(clusterID, regularInfraEnvID, 3, c.Name, c.BaseDNSDomain)
			Expect(len(hosts)).To(Equal(3))

			By("Waiting for cluster to become ready")
			waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, utils_test.ClusterInsufficientStateInfo)

			By("Registering a disconnected-iso InfraEnv on ready cluster")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("disconnected-infraenv-on-ready"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeDisconnectedIso,
					ClusterID:        &clusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()

			By("Verifying cluster remains in ready state (not changed to disconnected)")
			Consistently(func() string {
				resp, getErr := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				Expect(getErr).NotTo(HaveOccurred())
				return swag.StringValue(resp.GetPayload().Status)
			}, 5*time.Second, 1*time.Second).Should(Equal(models.ClusterStatusReady))
		})
	})

	Context("OVE discovery.ign download", func() {
		It("should fail downloading discovery.ign for unbound disconnected-iso infraenv", func() {
			By("Registering a disconnected-iso InfraEnv without cluster binding")
			infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("unbound-ove-infraenv"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeDisconnectedIso,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnv = infraEnvResp.GetPayload()
			infraEnvID = *infraEnv.ID

			By("Attempting to download discovery.ign")
			file, err := os.CreateTemp("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(file.Name())

			_, err = utils_test.TestContext.UserBMClient.Installer.V2DownloadInfraEnvFiles(ctx,
				&installer.V2DownloadInfraEnvFilesParams{
					InfraEnvID: infraEnvID,
					FileName:   "discovery.ign",
				}, file)

			By("Verifying it fails because infraenv is not bound to a cluster")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("404"))
		})
	})
})
