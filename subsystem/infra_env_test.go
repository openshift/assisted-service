package subsystem

import (
	"context"
	"io/ioutil"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Infra_Env", func() {
	ctx := context.Background()
	// var infraEnv *installer.RegisterInfraEnvCreated
	var infraEnv *models.InfraEnv
	var infraEnvID strfmt.UUID

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		res, err := userBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(sshPublicKey),
				ImageType:        models.ImageTypeFullIso,
			},
		})

		Expect(err).NotTo(HaveOccurred())
		infraEnv = res.GetPayload()
	})

	JustBeforeEach(func() {
		infraEnvID = *infraEnv.ID
	})

	It("download full-iso image success", func() {
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())
		_, err = userBMClient.Installer.DownloadInfraEnvDiscoveryImage(ctx, &installer.DownloadInfraEnvDiscoveryImageParams{InfraEnvID: infraEnvID}, file)
		Expect(err).NotTo(HaveOccurred())
	})

	It("update infra env", func() {
		time.Sleep(time.Second * 10)
		newSshKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56u" +
			"ar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvH" +
			"MAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqng" +
			"qbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3f" +
			"AC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAq" +
			"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
		updateParams := &installer.UpdateInfraEnvParams{
			InfraEnvID: infraEnvID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
				ImageType:              models.ImageTypeMinimalIso,
				IgnitionConfigOverride: `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`,
				SSHAuthorizedKey:       swag.String(newSshKey),
				Proxy:                  &models.Proxy{HTTPProxy: swag.String("http://proxy.proxy"), HTTPSProxy: nil, NoProxy: swag.String("proxy.proxy")},
			},
		}

		res, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
		Expect(err).NotTo(HaveOccurred())
		updateInfraEnv := res.Payload
		Expect(updateInfraEnv.SSHAuthorizedKey).To(Equal(newSshKey))
		Expect(swag.StringValue(updateInfraEnv.Proxy.HTTPProxy)).To(Equal("http://proxy.proxy"))
		Expect(swag.StringValue(updateInfraEnv.Proxy.HTTPSProxy)).To(Equal(""))
		Expect(swag.StringValue(updateInfraEnv.Proxy.NoProxy)).To(Equal("proxy.proxy"))
		Expect(updateInfraEnv.Type).To(Equal(models.ImageTypeMinimalIso))
	})

	It("download minimal-iso image success", func() {
		time.Sleep(time.Second * 10)
		_, err := userBMClient.Installer.UpdateInfraEnv(ctx,
			&installer.UpdateInfraEnvParams{InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}})
		Expect(err).NotTo(HaveOccurred())
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())
		_, err = userBMClient.Installer.DownloadInfraEnvDiscoveryImage(ctx, &installer.DownloadInfraEnvDiscoveryImageParams{InfraEnvID: infraEnvID}, file)
		Expect(err).NotTo(HaveOccurred())
	})

	It("download minimal-initrd success", func() {
		time.Sleep(time.Second * 10)
		_, err := userBMClient.Installer.UpdateInfraEnv(ctx,
			&installer.UpdateInfraEnvParams{InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}})
		Expect(err).NotTo(HaveOccurred())
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())
		_, _, err = userBMClient.Installer.DownloadMinimalInitrd(ctx, &installer.DownloadMinimalInitrdParams{InfraEnvID: infraEnvID}, file)
		Expect(err).NotTo(HaveOccurred())
	})

	It("download infra-env files discovery ignition file", func() {
		file, err := ioutil.TempFile("", "tmp")
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2DownloadInfraEnvFiles(ctx, &installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "discovery.ign"}, file)
		Expect(err).NotTo(HaveOccurred())
		s, err := file.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Size()).ShouldNot(Equal(0))

	})

	It("download infra-env files invalid filename option", func() {
		file, err := ioutil.TempFile("", "tmp")
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2DownloadInfraEnvFiles(ctx, &installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "bootstrap.ign"}, file)
		Expect(err).Should(HaveOccurred())

	})

	It("can list infra-envs", func() {
		resp, err := userBMClient.Installer.ListInfraEnvs(ctx, installer.NewListInfraEnvsParams())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(resp.Payload)).To(Equal(1))
		Expect(resp.Payload[0]).To(Equal(infraEnv))
	})

	It("deregister empty infra-env", func() {
		_, err := userBMClient.Installer.DeregisterInfraEnv(ctx, &installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
		Expect(err).NotTo(HaveOccurred())
	})

	It("deregister non-empty infra-env shold fail", func() {
		hostID := strToUUID(uuid.New().String())
		// register to infra-env
		_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.DeregisterInfraEnv(ctx, &installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})

		Expect(err).To(HaveOccurred())
	})
})
