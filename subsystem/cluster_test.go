package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"time"

	"github.com/filanov/bm-inventory/internal/bminventory"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/filanov/bm-inventory/client/installer"
	"github.com/filanov/bm-inventory/models"
)

const (
	clusterInsufficientStateInfo = "cluster is insufficient, exactly 3 known master hosts are needed for installation"
	clusterReadyStateInfo        = "Cluster ready to be installed"
)

var _ = Describe("Cluster tests", func() {
	ctx := context.Background()
	var cluster *installer.RegisterClusterCreated
	var clusterID strfmt.UUID
	var err error
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		cluster, err = bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test cluster"),
				OpenshiftVersion: swag.String("4.5"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("cluster CRUD", func() {
		_ = registerHost(clusterID)
		Expect(err).NotTo(HaveOccurred())

		getReply, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := bmclient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = bmclient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)

		publicKey := `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V5DVvyr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUAAbfmn/HalJTHB73LyjilKiPvJ+x2bG7AeiqyVHwtQSpt02FCdQGptmsSqqWF/b9botOO38eUsqPNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= oscohen@localhost.localdomain`

		c, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: &publicKey,
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{
						ID:   *host1.ID,
						Role: "master",
					},
					{
						ID:   *host2.ID,
						Role: "worker",
					},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(publicKey))

		h := getHost(clusterID, *host1.ID)
		Expect(h.Role).Should(Equal("master"))

		h = getHost(clusterID, *host2.ID)
		Expect(h.Role).Should(Equal("worker"))
	})
})

func waitForClusterState(ctx context.Context, clusterID strfmt.UUID, state string) {
	for start := time.Now(); time.Since(start) < 10*time.Second; {
		rep, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()
		if swag.StringValue(c.Status) == state {
			break
		}
		time.Sleep(time.Second)
	}
	rep, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := rep.GetPayload()
	Expect(swag.StringValue(c.Status)).Should(Equal(state))
}

func updateProgress(hostID strfmt.UUID, clusterID strfmt.UUID, progress string) {
	ctx := context.Background()
	installProgress := models.HostInstallProgressParams(progress)
	updateReply, err := bmclient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
		ClusterID:                 clusterID,
		HostInstallProgressParams: installProgress,
		HostID:                    hostID,
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
}

func installCluster(clusterID strfmt.UUID) {
	ctx := context.Background()
	_, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())

	rep, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := rep.GetPayload()
	Expect(swag.StringValue(c.Status)).Should(Equal("installing"))
	Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
	Expect(len(c.Hosts)).Should(Equal(4))
	for _, host := range c.Hosts {
		Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
	}

	for _, host := range c.Hosts {
		updateProgress(*host.ID, clusterID, "Done")
	}

	waitForClusterState(ctx, clusterID, "installed")
	rep, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c = rep.GetPayload()
	Expect(swag.StringValue(c.StatusInfo)).Should(Equal("installed"))

}

var _ = Describe("system-test cluster install", func() {
	var (
		ctx           = context.Background()
		cluster       *models.Cluster
		validDiskSize = int64(128849018880)
		clusterCIDR   = "10.128.0.0/14"
		serviceCIDR   = "172.30.0.0/16"
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		registerClusterReply, err := bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String("4.5"),
				PullSecret:               `{"auths":{"cloud.openshift.com":{"auth":""}}}`,
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
	})

	generateHWPostStepReply := func(h *models.Host, hwInfo *models.Inventory) {
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   string(hw),
				StepID:   string(models.StepTypeInventory),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}

	Context("install cluster cases", func() {
		var clusterID strfmt.UUID
		BeforeEach(func() {
			clusterID = *cluster.ID
			registerHostsAndSetRoles(clusterID, 4)
		})

		Context("install cluster", func() {

			It("install cluster", func() {
				_, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())

				rep, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal("installing"))
				Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
				Expect(len(c.Hosts)).Should(Equal(4))
				for _, host := range c.Hosts {
					Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
				}

				for _, host := range c.Hosts {
					updateProgress(*host.ID, clusterID, "Done")
				}

				waitForClusterState(ctx, clusterID, "installed")
				rep, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(swag.StringValue(c.StatusInfo)).Should(Equal("installed"))
			})
		})
		It("report_progress", func() {
			c, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())

			h := c.GetPayload().Hosts[0]

			By("progress_to_some_host", func() {
				installProgress := "installation step 1"
				updateProgress(*h.ID, clusterID, installProgress)
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installing-in-progress"))
				Expect(*h.StatusInfo).Should(Equal(installProgress))
			})

			By("progress_to_some_host_again", func() {
				installProgress := "installation step 2"
				updateProgress(*h.ID, clusterID, installProgress)
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installing-in-progress"))
				Expect(*h.StatusInfo).Should(Equal(installProgress))
			})

			By("report_done", func() {
				updateProgress(*h.ID, clusterID, "Done")
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installed"))
				Expect(*h.StatusInfo).Should(Equal("installed"))
			})

			By("report failed on other host", func() {
				h1 := c.GetPayload().Hosts[1]
				updateProgress(*h1.ID, clusterID, "Failed because some error")
				h1 = getHost(clusterID, *h1.ID)
				Expect(*h1.Status).Should(Equal("error"))
				Expect(*h1.StatusInfo).Should(Equal("Failed because some error"))
			})
		})

		It("install download_config_files", func() {

			//Test downloading kubeconfig files in worng state
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())

			defer os.Remove(file.Name())
			_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewDownloadClusterFilesConflict())))

			_, err = bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())

			missingClusterId := strfmt.UUID(uuid.New().String())
			_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: missingClusterId, FileName: "bootstrap.ign"}, file)
			Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterFilesNotFound())))

			_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "not_real_file"}, file)
			Expect(err).Should(HaveOccurred())

			_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("download_config_files in error state", func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(file.Name())

			c, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())

			By("report failed on a host", func() {
				h1 := c.GetPayload().Hosts[0]
				updateProgress(*h1.ID, clusterID, "Failed because some error")
				h1 = getHost(clusterID, *h1.ID)
				Expect(*h1.Status).Should(Equal("error"))
			})
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError)

			_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("Get credentials", func() {
			By("Test getting kubeadmin password for not found cluster")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := bmclient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: missingClusterId})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewGetCredentialsNotFound())))
			}
			By("Test getting kubeadmin password in wrong state")
			{
				_, err := bmclient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
				Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewGetCredentialsConflict())))
			}
			By("Test happy flow")
			{
				_, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				creds, err := bmclient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(creds.GetPayload().Username).To(Equal(bminventory.DefaultUser))
				Expect(creds.GetPayload().ConsoleURL).To(Equal(
					fmt.Sprintf("%s.%s.%s", bminventory.ConsoleUrlPrefix, cluster.Name, cluster.BaseDNSDomain)))
				Expect(len(creds.GetPayload().Password)).NotTo(Equal(0))
			}
		})

		It("Upload ingress ca and kubeconfig download", func() {
			ingressCa := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
				"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
				"MQswCQYDVQQHDAJkZDELMAkGA1UECgwCZGQxCzAJBgNVBAsMAmRkMQswCQYDVQQDDAJkZDERMA8GCSqGSIb3DQEJARYCZGQwHhcNMjAwNTI1MTYwNTAwWhcNMzA" +
				"wNTIzMTYwNTAwWjBhMQswCQYDVQQGEwJpczELMAkGA1UECAwCZGQxCzAJBgNVBAcMAmRkMQswCQYDVQQKDAJkZDELMAkGA1UECwwCZGQxCzAJBgNVBAMMAmRkMREwDwYJKoZIh" +
				"vcNAQkBFgJkZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAML63CXkBb+lvrJKfdfYBHLDYfuaC6exCSqASUAosJWWrfyDiDMUbmfs06PLKyv7N8efDhza74ov0EQJ" +
				"NRhMNaCE+A0ceq6ZXmmMswUYFdLAy8K2VMz5mroBFX8sj5PWVr6rDJ2ckBaFKWBB8NFmiK7MTWSIF9n8M107/9a0QURCvThUYu+sguzbsLODFtXUxG5rtTVKBVcPZvEfRky2Tkt4AySFS" +
				"mkO6Kf4sBd7MC4mKWZm7K8k7HrZYz2usSpbrEtYGtr6MmN9hci+/ITDPE291DFkzIcDCF493v/3T+7XsnmQajh6kuI+bjIaACfo8N+twEoJf/N1PmphAQdEiC0CAwEAAaNTMFEwHQYDVR0O" +
				"BBYEFNvmSprQQ2HUUtPxs6UOuxq9lKKpMB8GA1UdIwQYMBaAFNvmSprQQ2HUUtPxs6UOuxq9lKKpMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJEWxnxtQV5IqPVRr2SM" +
				"WNNxcJ7A/wyet39l5VhHjbrQGynk5WS80psn/riLUfIvtzYMWC0IR0pIMQuMDF5sNcKp4D8Xnrd+Bl/4/Iy/iTOoHlw+sPkKv+NL2XR3iO8bSDwjtjvd6L5NkUuzsRoSkQCG2fHASqqgFoyV9Ld" +
				"RsQa1w9ZGebtEWLuGsrJtR7gaFECqJnDbb0aPUMixmpMHID8kt154TrLhVFmMEqGGC1GvZVlQ9Of3GP9y7X4vDpHshdlWotOnYKHaeu2d5cRVFHhEbrslkISgh/TRuyl7VIpnjOYUwMBpCiVH6M" +
				"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
			By("Upload ingress ca for not existent clusterid")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := bmclient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: missingClusterId, IngressCertParams: "dummy"})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertNotFound())))
			}
			By("Test getting upload ingress ca in wrong state")
			{
				_, err := bmclient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: "dummy"})
				Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertBadRequest())))
			}
			By("Test happy flow")
			{

				installCluster(clusterID)
				// Download kubeconfig before uploading
				kubeconfigNoIngress, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = bmclient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "kubeconfig-noingress"}, kubeconfigNoIngress)
				Expect(err).NotTo(HaveOccurred())
				sni, err := kubeconfigNoIngress.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(sni.Size()).ShouldNot(Equal(0))

				res, err := bmclient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(reflect.TypeOf(res)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))

				// Download kubeconfig after uploading
				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = bmclient.Installer.DownloadClusterKubeconfig(ctx, &installer.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
				Expect(s.Size()).ShouldNot(Equal(sni.Size()))
			}
			By("Try to upload ingress ca second time, do nothing and return ok")
			{
				// Try to upload ingress ca second time
				res, err := bmclient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(reflect.TypeOf(res)).To(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))
			}
		})
	})

	It("install cluster requirement", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
			Disks: []*models.Disk{
				{DriveType: "SSD", Name: "loop0", SizeBytes: validDiskSize},
				{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
		}
		Expect(swag.StringValue(cluster.Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)
		apiVip := strfmt.IPv4("1.2.3.5")
		ingressVip := strfmt.IPv4("1.2.3.6")
		// All hosts are masters, one in discovering state  -> state must be insufficient
		cluster, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: "master"},
				{ID: *h2.ID, Role: "master"},
				{ID: *h4.ID, Role: "master"},
			},
				APIVip:     &apiVip,
				IngressVip: &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		// Adding one known host and setting as master -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterReadyStateInfo))

	})

	It("install_cluster_states", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
			Disks: []*models.Disk{
				{DriveType: "SSD", Name: "loop0", SizeBytes: validDiskSize},
				{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
		}
		Expect(swag.StringValue(cluster.Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		wh1 := registerHost(clusterID)
		generateHWPostStepReply(wh1, hwInfo)
		wh2 := registerHost(clusterID)
		generateHWPostStepReply(wh2, hwInfo)
		wh3 := registerHost(clusterID)
		generateHWPostStepReply(wh3, hwInfo)

		mh1 := registerHost(clusterID)
		generateHWPostStepReply(mh1, hwInfo)
		mh2 := registerHost(clusterID)
		generateHWPostStepReply(mh2, hwInfo)
		mh3 := registerHost(clusterID)
		generateHWPostStepReply(mh3, hwInfo)

		apiVip := strfmt.IPv4("1.2.3.5")
		ingressVip := strfmt.IPv4("1.2.3.6")
		// All hosts are workers -> state must be insufficient
		cluster, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *wh1.ID, Role: "worker"},
				{ID: *wh2.ID, Role: "worker"},
				{ID: *wh3.ID, Role: "worker"},
			},
				APIVip:     &apiVip,
				IngressVip: &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
		clusterReply, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterReply.Payload.APIVip).To(Equal(apiVip))
		Expect(clusterReply.Payload.MachineNetworkCidr).To(Equal("1.2.3.0/24"))
		Expect(len(clusterReply.Payload.HostNetworks)).To(Equal(1))
		Expect(clusterReply.Payload.HostNetworks[0].Cidr).To(Equal("1.2.3.0/24"))
		hids := make([]interface{}, 0)
		for _, h := range clusterReply.Payload.HostNetworks[0].HostIds {
			hids = append(hids, h)
		}
		Expect(len(hids)).To(Equal(6))
		Expect(*wh1.ID).To(BeElementOf(hids...))
		Expect(*wh2.ID).To(BeElementOf(hids...))
		Expect(*wh3.ID).To(BeElementOf(hids...))
		Expect(*mh1.ID).To(BeElementOf(hids...))
		Expect(*mh2.ID).To(BeElementOf(hids...))
		Expect(*mh3.ID).To(BeElementOf(hids...))

		// Only two masters -> state must be insufficient
		_, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh1.ID, Role: "master"},
				{ID: *mh2.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		// Three master hosts -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterReadyStateInfo))

		// Back to two master hosts -> state must be insufficient
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		// Three master hosts -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterReadyStateInfo))

		// Back to two master hosts -> state must be insufficient
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		_, err = bmclient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		_, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewGetClusterNotFound())))
	})

	It("install_cluster_insufficient_master", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 2},
			Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB)},
			Disks: []*models.Disk{
				{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
		}
		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		apiVip := strfmt.IPv4("1.2.3.8")
		ingressVip := strfmt.IPv4("1.2.3.9")
		_, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				APIVip:     &apiVip,
				IngressVip: &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).To(Not(HaveOccurred()))
		Expect(*getHost(clusterID, *h1.ID).Status).Should(Equal("known"))

		hwInfo = &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
		}
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)
		generateHWPostStepReply(h4, hwInfo)

		_, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: "master"},
				{ID: *h2.ID, Role: "master"},
				{ID: *h3.ID, Role: "master"},
				{ID: *h4.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))
	})
})

var _ = Describe("cluster install, with default network params", func() {
	var (
		ctx     = context.Background()
		cluster *models.Cluster
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		By("Register cluster")
		registerClusterReply, err := bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String("4.5"),
				PullSecret:       `{"auths":{"cloud.openshift.com":{"auth":""}}}`,
				SSHPublicKey:     "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
	})

	It("install cluster", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRoles(clusterID, 3)
		rep, err := bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()
		startTimeInstalling := c.InstallStartedAt
		startTimeInstalled := c.InstallCompletedAt

		_, err = bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		rep, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c = rep.GetPayload()
		Expect(swag.StringValue(c.Status)).Should(Equal("installing"))
		Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
		Expect(len(c.Hosts)).Should(Equal(3))
		Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		for _, host := range c.Hosts {
			Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
		}
		// fake installation completed
		for _, host := range c.Hosts {
			updateProgress(*host.ID, clusterID, "Done")
		}

		waitForClusterState(ctx, clusterID, "installed")
		rep, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c = rep.GetPayload()
		Expect(swag.StringValue(c.StatusInfo)).Should(Equal("installed"))
		Expect(c.InstallCompletedAt).ShouldNot(Equal(startTimeInstalled))
	})
})

func registerHostsAndSetRoles(clusterID strfmt.UUID, numHosts int) {
	validDiskSize := int64(128849018880)
	ctx := context.Background()

	hwInfo := &models.Inventory{
		CPU:    &models.CPU{Count: 16},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
		Disks: []*models.Disk{
			{DriveType: "SSD", Name: "loop0", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{"1.2.3.5/24"},
			},
		},
	}
	generateHWPostStepReply := func(h *models.Host, hwInfo *models.Inventory) {
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   string(hw),
				StepID:   string(models.StepTypeInventory),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}
	for i := 0; i < numHosts; i++ {
		host := registerHost(clusterID)
		generateHWPostStepReply(host, hwInfo)
		var role string
		if i < 3 {
			role = "master"
		} else {
			role = "worker"
		}
		_, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *host.ID, Role: role},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

	}
	apiVip := strfmt.IPv4("1.2.3.8")
	ingressVip := strfmt.IPv4("1.2.3.9")
	c, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			APIVip:     &apiVip,
			IngressVip: &ingressVip,
		},
		ClusterID: clusterID,
	})

	Expect(err).NotTo(HaveOccurred())
	Expect(swag.StringValue(c.GetPayload().Status)).Should(Equal("ready"))
	Expect(swag.StringValue(c.GetPayload().StatusInfo)).Should(Equal(clusterReadyStateInfo))
}
