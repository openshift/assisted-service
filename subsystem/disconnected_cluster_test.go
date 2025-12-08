package subsystem

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("Disconnected Cluster", func() {
	ctx := context.Background()

	BeforeEach(func() {
		utils_test.TestContext.DeregisterResources()
	})

	AfterEach(func() {
		utils_test.TestContext.DeregisterResources()
	})

	It("registers disconnected cluster and bound infraenv with rendezvous IP", func() {
		majorMinor := func(version string) string {
			parts := strings.Split(strings.TrimSpace(version), ".")
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return fmt.Sprintf("%s.%s", parts[0], parts[1])
			}
			return version
		}

		By("Creating a disconnected cluster via the dedicated endpoint")
		clusterReply, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterDisconnectedCluster(ctx, &installer.V2RegisterDisconnectedClusterParams{
			NewClusterParams: &models.DisconnectedClusterCreateParams{
				Name:             swag.String("ove-disconnected-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster := clusterReply.GetPayload()
		clusterID := *cluster.ID

		By("Verifying disconnected cluster properties")
		Expect(swag.StringValue(cluster.Status)).To(Equal(models.ClusterStatusUnmonitored))
		Expect(swag.StringValue(cluster.Kind)).To(Equal(models.ClusterKindDisconnectedCluster))
		Expect(swag.StringValue(cluster.StatusInfo)).To(Equal("Cluster created for offline installation"))
		Expect(cluster.OpenshiftVersion).To(Equal(openshiftVersion))

		By("Re-fetching the cluster to verify Kind and Status persistence")
		getClusterResp, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		fetchedCluster := getClusterResp.Payload
		Expect(swag.StringValue(fetchedCluster.Status)).To(Equal(models.ClusterStatusUnmonitored))
		Expect(swag.StringValue(fetchedCluster.Kind)).To(Equal(models.ClusterKindDisconnectedCluster))
		Expect(swag.StringValue(fetchedCluster.StatusInfo)).To(Equal("Cluster created for offline installation"))
		Expect(fetchedCluster.OpenshiftVersion).To(Equal(openshiftVersion))

		By("Registering a disconnected-iso InfraEnv bound to the cluster with rendezvous IP, proxy, and NTP")
		infraEnvResp, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("ove-infraenv-with-rendezvous"),
				OpenshiftVersion: openshiftVersion,
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
				ImageType:        models.ImageTypeDisconnectedIso,
				ClusterID:        &clusterID,
				RendezvousIP:     swag.String("192.168.1.100"),
				Proxy: &models.Proxy{
					HTTPProxy:  swag.String("http://proxy.example.com:8080"),
					HTTPSProxy: swag.String("http://proxy.example.com:8443"),
					NoProxy:    swag.String("localhost,127.0.0.1,.example.com"),
				},
				AdditionalNtpSources: swag.String("clock1.example.com,clock2.example.com"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		infraEnv := infraEnvResp.GetPayload()

		By("Verifying InfraEnv properties")
		Expect(swag.StringValue(infraEnv.Name)).To(Equal("ove-infraenv-with-rendezvous"))
		Expect(infraEnv.ClusterID).To(Equal(clusterID))
		Expect(swag.StringValue(infraEnv.RendezvousIP)).To(Equal("192.168.1.100"))
		Expect(common.ImageTypeValue(infraEnv.Type)).To(Equal(models.ImageTypeDisconnectedIso))
		Expect(majorMinor(infraEnv.OpenshiftVersion)).To(Equal(majorMinor(openshiftVersion)))
		Expect(infraEnv.CPUArchitecture).To(Equal("x86_64"))
		Expect(infraEnv.SSHAuthorizedKey).To(Equal(utils_test.SshPublicKey))
		Expect(infraEnv.PullSecretSet).To(BeTrue())
		Expect(infraEnv.DownloadURL).NotTo(BeEmpty())
		Expect(swag.StringValue(infraEnv.Proxy.HTTPProxy)).To(Equal("http://proxy.example.com:8080"))
		Expect(swag.StringValue(infraEnv.Proxy.HTTPSProxy)).To(Equal("http://proxy.example.com:8443"))
		Expect(swag.StringValue(infraEnv.Proxy.NoProxy)).To(Equal("localhost,127.0.0.1,.example.com"))
		Expect(infraEnv.AdditionalNtpSources).To(Equal("clock1.example.com,clock2.example.com"))
	})

	It("fails downloading discovery.ign for unbound disconnected-iso infraenv", func() {
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
		infraEnvID := *infraEnvResp.GetPayload().ID

		By("Attempting to download discovery.ign")
		file, err := os.CreateTemp("", "tmp")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}()

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
