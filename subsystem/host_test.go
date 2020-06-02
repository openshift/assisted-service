package subsystem

import (
	"context"

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
				Name:             swag.String("test cluster"),
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

	It("next step", func() {
		host := registerHost(clusterID)
		steps := getNextSteps(clusterID, *host.ID)
		_, ok := getStepInList(steps, models.StepTypeHardwareInfo)
		Expect(ok).Should(Equal(true))
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		host = getHost(clusterID, *host.ID)
		Expect(db.Model(host).Update("status", "known").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "disabled").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		Expect(len(steps)).Should(Equal(0))
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
		Expect(db.Model(host).Update("status", "disconnected").Error).NotTo(HaveOccurred())
		steps = getNextSteps(clusterID, *host.ID)
		_, ok = getStepInList(steps, models.StepTypeConnectivityCheck)
		Expect(ok).Should(Equal(true))
	})

	It("hardware_info_store_only_relevant_hw_reply", func() {
		host := registerHost(clusterID)

		extraHwInfo := "{\"extra\":\"data\",\"block_devices\":null,\"cpu\":{\"architecture\":\"x86_64\",\"cpus\":8,\"sockets\":1},\"memory\":[{\"available\":19743372,\"free\":8357316,\"name\":\"Mem\",\"shared\":1369116,\"total\":32657728,\"used\":11105024},{\"free\":16400380,\"name\":\"Swap\",\"total\":16400380}],\"nics\":[{\"cidrs\":[],\"mac\":\"f8:75:a4:a4:01:6e\",\"mtu\":1500,\"name\":\"enp0s31f6\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"80:32:53:4f:16:4f\",\"mtu\":1500,\"name\":\"wlp0s20f3\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"52:54:00:71:50:da\",\"mtu\":1500,\"name\":\"virbr1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"8e:59:a1:a9:14:23\",\"mtu\":1500,\"name\":\"virbr1-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"mask\":16}],\"mac\":\"02:42:aa:59:3a:d3\",\"mtu\":1500,\"name\":\"docker0\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[],\"mac\":\"fe:9b:ea:d0:f5:70\",\"mtu\":1500,\"name\":\"vnet0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"fe:16:a0:ea:b3:0b\",\"mtu\":1500,\"name\":\"vnet1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"}]}"
		hwInfo := "{\"block_devices\":null,\"cpu\":{\"architecture\":\"x86_64\",\"cpus\":8,\"sockets\":1},\"memory\":[{\"available\":19743372,\"free\":8357316,\"name\":\"Mem\",\"shared\":1369116,\"total\":32657728,\"used\":11105024},{\"free\":16400380,\"name\":\"Swap\",\"total\":16400380}],\"nics\":[{\"cidrs\":[],\"mac\":\"f8:75:a4:a4:01:6e\",\"mtu\":1500,\"name\":\"enp0s31f6\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"80:32:53:4f:16:4f\",\"mtu\":1500,\"name\":\"wlp0s20f3\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"52:54:00:71:50:da\",\"mtu\":1500,\"name\":\"virbr1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"8e:59:a1:a9:14:23\",\"mtu\":1500,\"name\":\"virbr1-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"mask\":24}],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"mask\":16}],\"mac\":\"02:42:aa:59:3a:d3\",\"mtu\":1500,\"name\":\"docker0\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[],\"mac\":\"fe:9b:ea:d0:f5:70\",\"mtu\":1500,\"name\":\"vnet0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"fe:16:a0:ea:b3:0b\",\"mtu\":1500,\"name\":\"vnet1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"}]}"

		_, err := bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   extraHwInfo,
				StepID:   string(models.StepTypeHardwareInfo),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(host.HardwareInfo).Should(Equal(hwInfo))

		_, err = bmclient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   "not a json",
				StepID:   string(models.StepTypeHardwareInfo),
			},
		})
		Expect(err).To(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(host.HardwareInfo).Should(Equal(hwInfo))
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

	It("disable enable", func() {
		host := registerHost(clusterID)
		_, err := bmclient.Installer.DisableHost(ctx, &installer.DisableHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("disabled"))
		Expect(len(getNextSteps(clusterID, *host.ID))).Should(Equal(0))

		_, err = bmclient.Installer.EnableHost(ctx, &installer.EnableHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering"))
		Expect(len(getNextSteps(clusterID, *host.ID))).ShouldNot(Equal(0))
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
				Name:             swag.String("another cluster"),
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

func getStepInList(steps models.Steps, sType models.StepType) (*models.Step, bool) {
	for _, step := range steps {
		if step.StepType == sType {
			return step, true
		}
	}
	return nil, false
}

func getNextSteps(clusterID, hostID strfmt.UUID) models.Steps {
	steps, err := bmclient.Installer.GetNextSteps(context.Background(), &installer.GetNextStepsParams{
		ClusterID: clusterID,
		HostID:    hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return steps.GetPayload()
}
