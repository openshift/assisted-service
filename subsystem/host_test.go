package subsystem

import (
	"context"
	"encoding/json"
	"time"

	"github.com/filanov/stateswitch/examples/host/host"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"

	"github.com/filanov/bm-inventory/client/installer"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Host tests", func() {
	ctx := context.Background()
	var cluster *installer.RegisterClusterCreated
	var clusterID strfmt.UUID

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		var err error
		cluster, err = bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String("4.5"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("host CRUD", func() {
		host := registerHost(clusterID)
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering"))
		Expect(host.StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		list, err := bmclient.Installer.ListHosts(ctx, &installer.ListHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Installer.DeregisterHost(ctx, &installer.DeregisterHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		list, err = bmclient.Installer.ListHosts(ctx, &installer.ListHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Installer.GetHost(ctx, &installer.GetHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).Should(HaveOccurred())
	})

	var defaultInventory = func() string {
		inventory := models.Inventory{
			Interfaces: []*models.Interface{
				{
					Name: "eth0",
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
					SpeedMbps: 20,
				},
				{
					Name: "eth1",
					IPV4Addresses: []string{
						"1.2.5.4/24",
					},
					SpeedMbps: 40,
				},
			},

			// CPU, Disks, and Memory were added here to prevent the case that bm-inventory crashes in case the monitor starts
			// working in the middle of the test and this inventory is in the database.
			CPU: &models.CPU{
				Count: 4,
			},
			Disks: []*models.Disk{
				{
					Name:      "sda1",
					DriveType: "HDD",
					SizeBytes: int64(120) * (int64(1) << 30),
				},
			},
			Memory: &models.Memory{
				PhysicalBytes: int64(16) * (int64(1) << 30),
			},
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		return string(b)
	}

	It("next step", func() {
		host := registerHost(clusterID)
		steps := getNextSteps(clusterID, *host.ID)
		_, ok := getStepInList(steps, models.StepTypeInventory)
		Expect(ok).Should(Equal(true))
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		host = getHost(clusterID, *host.ID)
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeInventory)
		Expect(ok).Should(Equal(true))
		_, ok = getStepInList(steps, models.StepTypeFreeNetworkAddresses)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "known").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		_, ok = getStepInList(steps, models.StepTypeFreeNetworkAddresses)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "disabled").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		Expect(steps.NextInstructionSeconds).Should(Equal(int64(120)))
		Expect(len(steps.Instructions)).Should(Equal(0))
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "disconnected").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "error").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeExecute)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", models.HostStatusResetting).Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeResetInstallation)
		Expect(ok).Should(Equal(true))
	})

	It("host installation progress", func() {
		host := registerHost(clusterID)
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())

		updateProgress(*host.ID, clusterID, models.HostStageStartingInstallation)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageStartingInstallation))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, clusterID, models.HostStageInstalling)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageInstalling))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, clusterID, models.HostStageWritingImageToDisk)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, clusterID, models.HostStageRebooting)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageRebooting))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, clusterID, models.HostStageConfiguring)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageConfiguring))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, clusterID, models.HostStageDone)
		host = getHost(clusterID, *host.ID)
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageDone))
		time.Sleep(time.Second * 3)
	})

	It("installation_error_reply", func() {
		host := registerHost(clusterID)
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "worker").Error).NotTo(HaveOccurred())

		_, err := bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: 137,
				Output:   "Failed to install",
				StepType: models.StepTypeInstall,
				StepID:   "installCmd-" + string(models.StepTypeExecute),
			},
		})
		Expect(err).Should(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("error"))
		Expect(swag.StringValue(host.StatusInfo)).Should(Equal("installation command failed"))

	})

	It("connectivity_report_store_only_relevant_reply", func() {
		host := registerHost(clusterID)

		connectivity := "{\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null}]}"
		extraConnectivity := "{\"extra\":\"data\",\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null}]}"

		_, err := bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   extraConnectivity,
				StepID:   string(models.StepTypeConnectivityCheck),
				StepType: models.StepTypeConnectivityCheck,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   "not a json",
				StepID:   string(models.StepTypeConnectivityCheck),
				StepType: models.StepTypeConnectivityCheck,
			},
		})
		Expect(err).To(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		//exit code is not 0
		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: -1,
				Error:    "some error",
				Output:   "not a json",
				StepID:   string(models.StepTypeConnectivityCheck),
			},
		})
		Expect(err).To(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

	})

	It("free addresses report", func() {
		h := registerHost(clusterID)

		free_addresses_report := "[{\"free_addresses\":[\"10.0.0.0\",\"10.0.0.1\"],\"network\":\"10.0.0.0/24\"},{\"free_addresses\":[\"10.0.1.0\"],\"network\":\"10.0.1.0/24\"}]"

		_, err := bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   free_addresses_report,
				StepID:   string(models.StepTypeFreeNetworkAddresses),
				StepType: models.StepTypeFreeNetworkAddresses,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Model(h).UpdateColumn("status", host.StateInsufficient).Error).NotTo(HaveOccurred())
		h = getHost(clusterID, *h.ID)
		Expect(h.FreeAddresses).Should(Equal(free_addresses_report))

		freeAddressesReply, err := bmclient.Installer.GetFreeAddresses(ctx, &installer.GetFreeAddressesParams{
			ClusterID: clusterID,
			Network:   "10.0.0.0/24",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(freeAddressesReply.Payload).To(HaveLen(2))
		Expect(freeAddressesReply.Payload[0]).To(Equal(strfmt.IPv4("10.0.0.0")))
		Expect(freeAddressesReply.Payload[1]).To(Equal(strfmt.IPv4("10.0.0.1")))

		freeAddressesReply, err = bmclient.Installer.GetFreeAddresses(ctx, &installer.GetFreeAddressesParams{
			ClusterID: clusterID,
			Network:   "10.0.1.0/24",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(freeAddressesReply.Payload).To(HaveLen(1))
		Expect(freeAddressesReply.Payload[0]).To(Equal(strfmt.IPv4("10.0.1.0")))

		freeAddressesReply, err = bmclient.Installer.GetFreeAddresses(ctx, &installer.GetFreeAddressesParams{
			ClusterID: clusterID,
			Network:   "10.0.2.0/24",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(freeAddressesReply.Payload).To(BeEmpty())

		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   "not a json",
				StepID:   string(models.StepTypeFreeNetworkAddresses),
				StepType: models.StepTypeFreeNetworkAddresses,
			},
		})
		Expect(err).To(HaveOccurred())
		h = getHost(clusterID, *h.ID)
		Expect(h.FreeAddresses).Should(Equal(free_addresses_report))

		//exit code is not 0
		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: -1,
				Error:    "some error",
				Output:   "not a json",
				StepID:   string(models.StepTypeFreeNetworkAddresses),
			},
		})
		Expect(err).To(HaveOccurred())
		h = getHost(clusterID, *h.ID)
		Expect(h.FreeAddresses).Should(Equal(free_addresses_report))
	})

	It("disable enable", func() {
		host := registerHost(clusterID)
		_, err := bmclient.Installer.DisableHost(ctx, &installer.DisableHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("disabled"))
		Expect(len(getNextSteps(clusterID, *host.ID).Instructions)).Should(Equal(0))

		_, err = bmclient.Installer.EnableHost(ctx, &installer.EnableHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering"))
		Expect(len(getNextSteps(clusterID, *host.ID).Instructions)).ShouldNot(Equal(0))
	})

	It("debug", func() {
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)
		// set debug to host1
		_, err := bmclient.Installer.SetDebugStep(ctx, &installer.SetDebugStepParams{
			ClusterID: clusterID,
			HostID:    *host1.ID,
			Step:      &models.DebugStep{Command: swag.String("echo hello")},
		})
		Expect(err).NotTo(HaveOccurred())

		var step *models.Step
		var ok bool
		// debug should be only for host1
		_, ok = getStepInList(getNextSteps(clusterID, *host2.ID), models.StepTypeExecute)
		Expect(ok).Should(Equal(false))

		step, ok = getStepInList(getNextSteps(clusterID, *host1.ID), models.StepTypeExecute)
		Expect(ok).Should(Equal(true))
		Expect(step.Command).Should(Equal("bash"))
		Expect(step.Args).Should(Equal([]string{"-c", "echo hello"}))

		// debug executed only once
		_, ok = getStepInList(getNextSteps(clusterID, *host1.ID), models.StepTypeExecute)
		Expect(ok).Should(Equal(false))

		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host1.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   "hello",
				StepID:   step.StepID,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("register_same_host_id", func() {
		hostID := strToUUID(uuid.New().String())
		// register to cluster1
		_, err := bmclient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
			ClusterID: clusterID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cluster2, err := bmclient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("another-cluster"),
				OpenshiftVersion: swag.String("4.5"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// register to cluster2
		_, err = bmclient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
			ClusterID: *cluster2.GetPayload().ID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// successfully get from both clusters
		_ = getHost(clusterID, *hostID)
		_ = getHost(*cluster2.GetPayload().ID, *hostID)

		_, err = bmclient.Installer.DeregisterHost(ctx, &installer.DeregisterHostParams{
			ClusterID: clusterID,
			HostID:    *hostID,
		})
		Expect(err).NotTo(HaveOccurred())
		h := getHost(*cluster2.GetPayload().ID, *hostID)

		// register again to cluster 2 and expect it to be in discovery status
		Expect(db.Model(h).Update("status", "known").Error).NotTo(HaveOccurred())
		h = getHost(*cluster2.GetPayload().ID, *hostID)
		Expect(swag.StringValue(h.Status)).Should(Equal("known"))
		_, err = bmclient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
			ClusterID: *cluster2.GetPayload().ID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(*cluster2.GetPayload().ID, *hostID)
		Expect(swag.StringValue(h.Status)).Should(Equal("discovering"))
	})
})
