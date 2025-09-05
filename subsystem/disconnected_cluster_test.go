package subsystem

import (
    "context"
    "os"
    "time"

    "github.com/go-openapi/strfmt"
    "github.com/go-openapi/swag"
    . "github.com/onsi/ginkgo"
    . "github.com/onsi/gomega"
    "github.com/openshift/assisted-service/client/installer"
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

    It("registers with Kind=DisconnectedCluster and Status=Created", func() {
        By("Creating a disconnected cluster via the dedicated endpoint")
        reply, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterDisconnectedCluster(ctx, &installer.V2RegisterDisconnectedClusterParams{
            NewClusterParams: &models.ClusterCreateParams{
                Name:             swag.String("ove-disconnected-cluster"),
                OpenshiftVersion: swag.String(openshiftVersion),
                PullSecret:       swag.String(pullSecret),
                BaseDNSDomain:    "example.com",
            },
        })
        Expect(err).NotTo(HaveOccurred())
        c := reply.GetPayload()

        By("Verifying initial status and kind are set on creation")
        Expect(swag.StringValue(c.Status)).To(Equal(models.ClusterStatusCreated))
        Expect(swag.StringValue(c.Kind)).To(Equal(models.ClusterKindDisconnectedCluster))
        Expect(swag.StringValue(c.StatusInfo)).To(Equal("Cluster created for offline installation"))

        By("Fetching the cluster and re-validating")
        got, err := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: *c.ID})
        Expect(err).NotTo(HaveOccurred())
        Expect(swag.StringValue(got.Payload.Status)).To(Equal(models.ClusterStatusCreated))
        Expect(swag.StringValue(got.Payload.Kind)).To(Equal(models.ClusterKindDisconnectedCluster))
        Expect(swag.StringValue(got.Payload.StatusInfo)).To(Equal("Cluster created for offline installation"))
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
