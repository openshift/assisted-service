package subsystem

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"reflect"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/client/installer"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
				OpenshiftVersion: swag.String("4.4"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
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
				SSHPublicKey: publicKey,
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

var _ = Describe("system-test cluster install", func() {
	var (
		ctx           = context.Background()
		cluster       *models.Cluster
		validDiskSize = int64(128849018880)
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		registerClusterReply, err := bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       "10.128.0.0/14",
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String("4.4"),
				PullSecret:               `{"auths":{"cloud.openshift.com":{"auth":""}}}`,
				ServiceNetworkCidr:       "172.30.0.0/16",
				SSHPublicKey:             "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
	})

	generateHWPostStepReply := func(h *models.Host, hwInfo *models.Introspection) {
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   string(hw),
				StepID:   string(models.StepTypeHardwareInfo),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}

	Context("install cluster cases", func() {
		var clusterID strfmt.UUID
		BeforeEach(func() {
			clusterID = *cluster.ID

			hwInfo := &models.Introspection{
				CPU:    &models.CPUDetails{Cpus: 16},
				Memory: []*models.MemoryDetails{{Name: "Mem", Total: int64(32 * units.GiB)}},
				BlockDevices: []*models.BlockDevice{
					{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "loop0", ReadOnly: true, RemovableDevice: 1, Size: validDiskSize},
					{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize}},
			}

			h1 := registerHost(clusterID)
			generateHWPostStepReply(h1, hwInfo)
			h2 := registerHost(clusterID)
			generateHWPostStepReply(h2, hwInfo)
			h3 := registerHost(clusterID)
			generateHWPostStepReply(h3, hwInfo)
			h4 := registerHost(clusterID)
			generateHWPostStepReply(h4, hwInfo)
			c, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{ID: *h1.ID, Role: "master"},
					{ID: *h2.ID, Role: "master"},
					{ID: *h3.ID, Role: "master"},
					{ID: *h4.ID, Role: "worker"},
				}},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(c.GetPayload().Status)).Should(Equal("ready"))
		})

		It("install cluster", func() {
			c, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(c.GetPayload().Status)).Should(Equal("installing"))
			Expect(len(c.GetPayload().Hosts)).Should(Equal(4))
			for _, host := range c.GetPayload().Hosts {
				Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
			}
		})

		It("host install fails while install cluster", func() {
			c, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(c.GetPayload().Status)).Should(Equal("installing"))
			Expect(len(c.GetPayload().Hosts)).Should(Equal(4))
			for _, host := range c.GetPayload().Hosts {
				Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
			}
		})

		It("report_progress", func() {
			c, err := bmclient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())

			h := c.GetPayload().Hosts[0]

			updateProgress := func(hostID strfmt.UUID, progress string) {
				installProgress := models.HostInstallProgressParams(progress)
				updateReply, err := bmclient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
					ClusterID:                 clusterID,
					HostInstallProgressParams: installProgress,
					HostID:                    hostID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
			}

			By("progress_to_some_host", func() {
				installProgress := "installation step 1"
				updateProgress(*h.ID, installProgress)
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installing-in-progress"))
				Expect(*h.StatusInfo).Should(Equal(installProgress))
			})

			By("progress_to_some_host_again", func() {
				installProgress := "installation step 2"
				updateProgress(*h.ID, installProgress)
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installing-in-progress"))
				Expect(*h.StatusInfo).Should(Equal(installProgress))
			})

			By("report_done", func() {
				updateProgress(*h.ID, "Done")
				h = getHost(clusterID, *h.ID)
				Expect(*h.Status).Should(Equal("installed"))
				Expect(*h.StatusInfo).Should(Equal("installed"))
			})

			By("report failed on other host", func() {
				h1 := c.GetPayload().Hosts[1]
				updateProgress(*h1.ID, "Failed because some error")
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
	})

	It("install cluster requirement", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Introspection{
			CPU:    &models.CPUDetails{Cpus: 16},
			Memory: []*models.MemoryDetails{{Name: "Mem", Total: int64(32 * units.GiB)}},
			BlockDevices: []*models.BlockDevice{
				{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "loop0", ReadOnly: true, RemovableDevice: 1, Size: validDiskSize},
				{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize}},
		}
		Expect(swag.StringValue(cluster.Status)).Should(Equal("insufficient"))

		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)

		// All hosts are masters, one in discovering state  -> state must be insufficient
		cluster, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: "master"},
				{ID: *h2.ID, Role: "master"},
				{ID: *h4.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))

		// Adding one known host and setting as master -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))
	})

	It("install_cluster_states", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Introspection{
			CPU:    &models.CPUDetails{Cpus: 16},
			Memory: []*models.MemoryDetails{{Name: "Mem", Total: int64(32 * units.GiB)}},
			BlockDevices: []*models.BlockDevice{
				{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "loop0", ReadOnly: true, RemovableDevice: 1, Size: validDiskSize},
				{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize}},
		}
		Expect(swag.StringValue(cluster.Status)).Should(Equal("insufficient"))

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

		// All hosts are workers -> state must be insufficient
		cluster, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *wh1.ID, Role: "worker"},
				{ID: *wh2.ID, Role: "worker"},
				{ID: *wh3.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))

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

		// Three master hosts -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))

		// Back to two master hosts -> state must be insufficient
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))

		// Three master hosts -> state must be ready
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "master"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("ready"))

		// Back to two master hosts -> state must be insufficient
		cluster, err = bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))

		_, err = bmclient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		_, err = bmclient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewGetClusterNotFound())))
	})

	It("install_cluster_insufficient_master", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Introspection{
			CPU:    &models.CPUDetails{Cpus: 2},
			Memory: []*models.MemoryDetails{{Name: "Mem", Total: int64(8 * units.GiB)}},
			BlockDevices: []*models.BlockDevice{
				{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize}},
		}
		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		Expect(*getHost(clusterID, *h1.ID).Status).Should(Equal("known"))

		hwInfo = &models.Introspection{
			CPU:    &models.CPUDetails{Cpus: 16},
			Memory: []*models.MemoryDetails{{Name: "Mem", Total: int64(32 * units.GiB)}},
		}
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)
		generateHWPostStepReply(h4, hwInfo)

		_, err := bmclient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
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
