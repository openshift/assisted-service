package subsystem

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var registerInfraEnv = func(clusterID *strfmt.UUID, imageType models.ImageType) *models.InfraEnv {
	request, err := userBMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
		InfraenvCreateParams: &models.InfraEnvCreateParams{
			Name:             swag.String("test-infra-env"),
			OpenshiftVersion: openshiftVersion,
			PullSecret:       swag.String(pullSecret),
			SSHAuthorizedKey: swag.String(sshPublicKey),
			ImageType:        imageType,
			ClusterID:        clusterID,
		},
	})

	Expect(err).NotTo(HaveOccurred())
	return request.GetPayload()
}

var _ = Describe("Infra_Env", func() {
	ctx := context.Background()
	var (
		infraEnv   *models.InfraEnv
		infraEnvID strfmt.UUID

		infraEnv2 *models.InfraEnv
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		infraEnv = registerInfraEnv(nil, models.ImageTypeFullIso)
		clusterResp, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				BaseDNSDomain:            "example.com",
				ClusterNetworkHostPrefix: 23,
			},
		})

		Expect(err).NotTo(HaveOccurred())
		clusterID = *clusterResp.GetPayload().ID
		infraEnv2 = registerInfraEnv(&clusterID, models.ImageTypeFullIso)
	})

	JustBeforeEach(func() {
		infraEnvID = *infraEnv.ID
	})

	getInfraEnv := func() {
		resp, err := userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
		Expect(err).NotTo(HaveOccurred())

		infraEnv = resp.Payload
	}

	It("download full-iso image success", func() {
		getInfraEnv()
		downloadIso(ctx, infraEnv.DownloadURL)
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
		Expect(swag.StringValue(updateInfraEnv.Proxy.HTTPSProxy)).To(Equal("http://proxy.proxy"))
		Expect(swag.StringValue(updateInfraEnv.Proxy.NoProxy)).To(Equal("proxy.proxy"))
		Expect(common.ImageTypeValue(updateInfraEnv.Type)).To(Equal(models.ImageTypeMinimalIso))
	})

	It("download minimal-iso image success", func() {
		_, err := userBMClient.Installer.UpdateInfraEnv(ctx,
			&installer.UpdateInfraEnvParams{InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}})
		Expect(err).NotTo(HaveOccurred())
		getInfraEnv()
		downloadIso(ctx, infraEnv.DownloadURL)
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
		Expect(len(resp.Payload)).To(Equal(2))
		Expect(resp.Payload).To(ContainElement(infraEnv))
		Expect(resp.Payload).To(ContainElement(infraEnv2))
	})

	It("can list infra-envs by cluster id", func() {
		resp, err := userBMClient.Installer.ListInfraEnvs(ctx, &installer.ListInfraEnvsParams{ClusterID: &clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(resp.Payload)).To(Equal(1))
		Expect(resp.Payload[0]).To(Equal(infraEnv2))
	})

	It("deregister empty infra-env", func() {
		_, err := userBMClient.Installer.DeregisterInfraEnv(ctx, &installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
		Expect(err).NotTo(HaveOccurred())
	})

	It("deregister non-empty infra-env should fail", func() {
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

	It("can get ipxe script", func() {
		buf := &bytes.Buffer{}
		_, err := userBMClient.Installer.V2DownloadInfraEnvFiles(ctx, &installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "ipxe-script"}, buf)
		Expect(err).NotTo(HaveOccurred())

		script := buf.String()
		Expect(script).To(HavePrefix("#!ipxe"))
	})

	It("can get ipxe script presigned url", func() {
		res, err := userBMClient.Installer.GetInfraEnvPresignedFileURL(ctx, &installer.GetInfraEnvPresignedFileURLParams{InfraEnvID: infraEnvID, FileName: "ipxe-script"})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Payload).ToNot(BeNil())
		u := res.Payload.URL
		Expect(u).NotTo(BeNil())

		scriptResp, err := http.Get(*u)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusOK))
		script, err := io.ReadAll(scriptResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(script).To(HavePrefix("#!ipxe"))
	})
})
