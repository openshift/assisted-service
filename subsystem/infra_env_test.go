package subsystem

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var registerInfraEnv = func(clusterID *strfmt.UUID, imageType models.ImageType) *models.InfraEnv {
	return internalRegisterInfraEnv(clusterID, imageType, "", openshiftVersion)
}

var registerInfraEnvSpecificVersionAndArch = func(clusterID *strfmt.UUID, imageType models.ImageType, cpuArch, ocpVersion string) *models.InfraEnv {
	return internalRegisterInfraEnv(clusterID, imageType, cpuArch, ocpVersion)
}

var internalRegisterInfraEnv = func(clusterID *strfmt.UUID, imageType models.ImageType, cpuArch, ocpVersion string) *models.InfraEnv {
	request, err := userBMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
		InfraenvCreateParams: &models.InfraEnvCreateParams{
			Name:             swag.String("test-infra-env"),
			OpenshiftVersion: ocpVersion,
			PullSecret:       swag.String(pullSecret),
			SSHAuthorizedKey: swag.String(sshPublicKey),
			ImageType:        imageType,
			ClusterID:        clusterID,
			CPUArchitecture:  cpuArch,
		},
	})

	Expect(err).NotTo(HaveOccurred())
	return request.GetPayload()
}

func validateKernelArgs(infraEnv *models.InfraEnv, expectedKargs models.KernelArguments) {
	if expectedKargs == nil {
		Expect(infraEnv.KernelArguments).To(BeNil())
	} else {
		Expect(infraEnv.KernelArguments).ToNot(BeNil())
		var kargs models.KernelArguments
		Expect(json.Unmarshal([]byte(*infraEnv.KernelArguments), &kargs)).ToNot(HaveOccurred())
		Expect(kargs).To(Equal(expectedKargs))
	}
}

var _ = Describe("Register InfraEnv- kernel arguments", func() {
	register := func(kernelArgs models.KernelArguments) (*installer.RegisterInfraEnvCreated, error) {
		return userBMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: openshiftVersion,
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(sshPublicKey),
				ImageType:        models.ImageTypeMinimalIso,
				KernelArguments:  kernelArgs,
			},
		})
	}

	It("happy flow", func() {
		kargs := models.KernelArguments{
			{
				Operation: models.KernelArgumentOperationAppend,
				Value:     "p1",
			},
			{
				Operation: models.KernelArgumentOperationAppend,
				Value:     `p2="this is an argument"`,
			},
		}
		res, err := register(kargs)
		Expect(err).ToNot(HaveOccurred())
		validateKernelArgs(res.Payload, kargs)
	})

	DescribeTable("unsupported cases",
		func(operation, value string) {
			kargs := models.KernelArguments{
				{
					Operation: operation,
					Value:     value,
				},
			}
			_, err := register(kargs)
			Expect(err).To(HaveOccurred())
		},
		Entry("unsupported replace operation", models.KernelArgumentOperationReplace, "p1"),
		Entry("unsupported delete operation", models.KernelArgumentOperationDelete, "p1"),
		Entry("illegal operation", "illegal", "p1"),
		Entry("value is illegal", models.KernelArgumentOperationAppend, "illegal value with unquoted space"),
	)
})

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
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				BaseDNSDomain:    "example.com",
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

	Context("update kernel arguments", func() {
		It("update kernel arguments scenarios", func() {
			kargs1 := models.KernelArguments{
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p1",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p2",
				},
			}

			By("setting kernel arguments")
			updateParams := &installer.UpdateInfraEnvParams{
				InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
					KernelArguments: kargs1,
				},
			}
			res, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
			Expect(err).NotTo(HaveOccurred())
			validateKernelArgs(res.Payload, kargs1)

			By("override kernel arguments with new ones")
			kargs2 := models.KernelArguments{
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p3",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p4",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     `p1="this is an argument"`,
				},
			}
			updateParams.InfraEnvUpdateParams.KernelArguments = kargs2
			res, err = userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
			Expect(err).NotTo(HaveOccurred())
			validateKernelArgs(res.Payload, kargs2)

			By("update without setting value")
			updateParams.InfraEnvUpdateParams.KernelArguments = nil

			// Need to update with some field other than kernel arguments
			updateParams.InfraEnvUpdateParams.PullSecret = pullSecret
			res, err = userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
			Expect(err).NotTo(HaveOccurred())
			validateKernelArgs(res.Payload, kargs2)

			By("clear existing kernel arguments")
			// Return to default
			updateParams.InfraEnvUpdateParams.PullSecret = ""
			updateParams.InfraEnvUpdateParams.KernelArguments = make(models.KernelArguments, 0)
			res, err = userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
			Expect(err).NotTo(HaveOccurred())
			validateKernelArgs(res.Payload, nil)
		})
		DescribeTable("unsupported cases",
			func(operation, value string) {
				updateParams := &installer.UpdateInfraEnvParams{
					InfraEnvID: infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						KernelArguments: models.KernelArguments{
							{
								Operation: operation,
								Value:     value,
							},
						},
					},
				}
				_, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
				Expect(err).To(HaveOccurred())
			},
			Entry("unsupported replace operation", models.KernelArgumentOperationReplace, "p1"),
			Entry("unsupported delete operation", models.KernelArgumentOperationDelete, "p1"),
			Entry("illegal operation", "illegal", "p1"),
			Entry("value is illegal", models.KernelArgumentOperationAppend, "illegal value with unquoted space"),
		)
	})

	It("update infra env with NoProxy wildcard", func() {
		updateParams := &installer.UpdateInfraEnvParams{
			InfraEnvID: infraEnvID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
				Proxy: &models.Proxy{NoProxy: swag.String("*")},
			},
		}
		res, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
		Expect(err).NotTo(HaveOccurred())
		updateInfraEnv := res.Payload
		Expect(swag.StringValue(updateInfraEnv.Proxy.NoProxy)).To(Equal("*"))
	})

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
		Expect(swag.StringValue(updateInfraEnv.Proxy.HTTPSProxy)).To(BeEmpty())
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
		file, err := os.CreateTemp("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())
		_, _, err = userBMClient.Installer.DownloadMinimalInitrd(ctx, &installer.DownloadMinimalInitrdParams{InfraEnvID: infraEnvID}, file)
		Expect(err).NotTo(HaveOccurred())
	})

	It("download infra-env files discovery ignition file", func() {
		file, err := os.CreateTemp("", "tmp")
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2DownloadInfraEnvFiles(ctx, &installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "discovery.ign"}, file)
		Expect(err).NotTo(HaveOccurred())
		s, err := file.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Size()).ShouldNot(Equal(0))
	})

	It("download infra-env files static network config file", func() {
		By("Patching the infra env with a static network config")
		netYaml := `interfaces:
- ipv4:
    address:
    - ip: 192.0.2.1
      prefix-length: 24
    dhcp: false
    enabled: true
  name: eth0
  state: up
  type: ethernet`
		staticNetworkConfig := models.HostStaticNetworkConfig{
			NetworkYaml: netYaml,
		}
		staticNetworkConfigs := []*models.HostStaticNetworkConfig{&staticNetworkConfig}
		updateParams := &installer.UpdateInfraEnvParams{
			InfraEnvID: infraEnvID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
				StaticNetworkConfig: staticNetworkConfigs,
			},
		}
		_, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
		Expect(err).NotTo(HaveOccurred())
		By("Downloading the static network config archive")
		buf := &bytes.Buffer{}
		_, err = userBMClient.Installer.V2DownloadInfraEnvFiles(ctx, &installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "static-network-config"}, buf)
		Expect(err).NotTo(HaveOccurred())
		By("Verifying the contents of the archive")
		contents := buf.String()
		Expect(len(contents)).ShouldNot(Equal(0))
		Expect(contents).To(ContainSubstring("/etc/assisted/network/host0"))
		Expect(contents).To(ContainSubstring("192.0.2.1/24"))
		Expect(contents).To(ContainSubstring("eth0"))
	})

	It("download infra-env files invalid filename option", func() {
		file, err := os.CreateTemp("", "tmp")
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

		Expect(*u).ToNot(ContainSubstring("boot_control"))
		scriptResp, err := http.Get(*u)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusOK))
		script, err := io.ReadAll(scriptResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(script).To(HavePrefix("#!ipxe"))
	})
	It("ipxe with boot control", func() {
		res, err := userBMClient.Installer.GetInfraEnvPresignedFileURL(ctx, &installer.GetInfraEnvPresignedFileURLParams{
			InfraEnvID:     infraEnvID,
			FileName:       "ipxe-script",
			IpxeScriptType: swag.String(bminventory.BootOrderControl)})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Payload).ToNot(BeNil())
		url := swag.StringValue(res.Payload.URL)
		Expect(url).NotTo(BeEmpty())

		By("Serve redirect script")
		scriptResp, err := http.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusOK))
		script, err := io.ReadAll(scriptResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(script).To(HavePrefix("#!ipxe"))
		re := regexp.MustCompile(`chain +([^ \n\t]+[?&]mac=[$]{net0/mac}(?:&[^ \n\t]+)?)`)
		matches := re.FindStringSubmatch(string(script))
		Expect(matches).To(HaveLen(2))
		url = strings.ReplaceAll(matches[1], "${net0/mac}", "e6:53:3d:a7:77:b4")

		By("host does not exist")
		scriptResp, err = http.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusOK))
		script, err = io.ReadAll(scriptResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(script).To(HavePrefix("#!ipxe"))
		Expect(string(script)).To(MatchRegexp(`.*initrd --name initrd.*`))

		By("Create host")
		hostID := strToUUID(uuid.New().String())
		// register to infra-env
		response, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		host := &response.Payload.Host
		generateHWPostStepReply(context.Background(), host, getValidWorkerHwInfoWithCIDR("1.2.3.4/24"), "h1")

		By("host is insufficient")
		scriptResp, err = http.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusOK))
		script, err = io.ReadAll(scriptResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(script).To(HavePrefix("#!ipxe"))
		Expect(string(script)).To(MatchRegexp(`.*initrd --name initrd.*`))

		By("host is installed")
		Expect(db.Model(&models.Host{}).Where("id = ? and infra_env_id = ?", hostID.String(), infraEnvID.String()).
			Update("status", models.HostStatusInstalled).Error).ToNot(HaveOccurred())
		scriptResp, err = http.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusNotFound))

		By("duplicate mac")
		Expect(db.Model(&models.Host{}).Where("id = ? and infra_env_id = ?", hostID.String(), infraEnvID.String()).
			Update("status", models.HostStatusInsufficient).Error).ToNot(HaveOccurred())

		hostID2 := strToUUID(uuid.New().String())
		// register to infra-env
		response, err = agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID2,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		host2 := &response.Payload.Host
		generateHWPostStepReply(context.Background(), host2, getValidWorkerHwInfoWithCIDR("1.2.3.5/24"), "h2")
		scriptResp, err = http.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptResp.StatusCode).To(Equal(http.StatusInternalServerError))
	})

	It("fails when given invalid static network config", func() {
		netYaml := "interfaces:\n    - foo: badConfig"
		staticNetworkConfig := models.HostStaticNetworkConfig{
			NetworkYaml: netYaml,
		}
		staticNetworkConfigs := []*models.HostStaticNetworkConfig{&staticNetworkConfig}
		updateParams := &installer.UpdateInfraEnvParams{
			InfraEnvID: infraEnvID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
				StaticNetworkConfig: staticNetworkConfigs,
			},
		}
		_, err := userBMClient.Installer.UpdateInfraEnv(ctx, updateParams)
		Expect(err).To(HaveOccurred())
	})
})
