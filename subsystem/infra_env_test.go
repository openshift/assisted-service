package subsystem

import (
	"context"
	"io/ioutil"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

// #nosec
const (
	infraEnvSshPublicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain"
	infraEnvPullSecret   = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
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
				PullSecret:       swag.String(infraEnvPullSecret),
				SSHAuthorizedKey: swag.String(infraEnvSshPublicKey),
				ImageType:        models.ImageTypeFullIso,
			},
		})

		Expect(err).NotTo(HaveOccurred())
		infraEnv = res.GetPayload()
	})

	JustBeforeEach(func() {
		infraEnvID = infraEnv.ID
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
})
