package subsystem

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Host tests", func() {
	ctx := context.Background()
	var cluster *inventory.RegisterClusterCreated
	var clusterID strfmt.UUID

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		var err error
		cluster, err = bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name: swag.String("test cluster"),
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

		list, err := bmclient.Inventory.ListHosts(ctx, &inventory.ListHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterHost(ctx, &inventory.DeregisterHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		list, err = bmclient.Inventory.ListHosts(ctx, &inventory.ListHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{
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
	})

	It("hardware-info store only relevant hw reply", func() {
		host := registerHost(clusterID)

		extraHwInfo := "{\"extra\":\"data\",\"block-devices\":[{\"device-type\":\"disk\",\"major-device-number\":259,\"name\":\"nvme0n1\",\"size\":256060514304},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":1,\"name\":\"nvme0n1p1\",\"size\":629145600},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":2,\"name\":\"nvme0n1p2\",\"size\":1073741824},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":3,\"name\":\"nvme0n1p3\",\"size\":254356226048}],\"cpu\":{\"architecture\":\"x86_64\",\"cpu-mhz\":1532.999,\"cpus\":8,\"model-name\":\"Intel(R) Core(TM) i7-8665U CPU @ 1.90GHz\",\"sockets\":1,\"threads-per-core\":2},\"memory\":[{\"available\":19743372,\"buff-cached\":13195388,\"free\":8357316,\"name\":\"Mem\",\"shared\":1369116,\"total\":32657728,\"used\":11105024},{\"free\":16400380,\"name\":\"Swap\",\"total\":16400380}],\"nics\":[{\"cidrs\":[],\"mac\":\"f8:75:a4:a4:01:6e\",\"mtu\":1500,\"name\":\"enp0s31f6\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[{\"ip-address\":\"10.100.102.12\",\"mask\":24}],\"mac\":\"80:32:53:4f:16:4f\",\"mtu\":1500,\"name\":\"wlp0s20f3\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[{\"ip-address\":\"192.168.39.1\",\"mask\":24}],\"mac\":\"52:54:00:71:50:da\",\"mtu\":1500,\"name\":\"virbr1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"8e:59:a1:a9:14:23\",\"mtu\":1500,\"name\":\"virbr1-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"ip-address\":\"192.168.122.1\",\"mask\":24}],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"ip-address\":\"172.17.0.1\",\"mask\":16}],\"mac\":\"02:42:aa:59:3a:d3\",\"mtu\":1500,\"name\":\"docker0\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[],\"mac\":\"fe:9b:ea:d0:f5:70\",\"mtu\":1500,\"name\":\"vnet0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"fe:16:a0:ea:b3:0b\",\"mtu\":1500,\"name\":\"vnet1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"}]}"
		hwInfo := "{\"block-devices\":[{\"device-type\":\"disk\",\"major-device-number\":259,\"name\":\"nvme0n1\",\"size\":256060514304},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":1,\"name\":\"nvme0n1p1\",\"size\":629145600},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":2,\"name\":\"nvme0n1p2\",\"size\":1073741824},{\"device-type\":\"part\",\"major-device-number\":259,\"minor-device-number\":3,\"name\":\"nvme0n1p3\",\"size\":254356226048}],\"cpu\":{\"architecture\":\"x86_64\",\"cpu-mhz\":1532.999,\"cpus\":8,\"model-name\":\"Intel(R) Core(TM) i7-8665U CPU @ 1.90GHz\",\"sockets\":1,\"threads-per-core\":2},\"memory\":[{\"available\":19743372,\"buff-cached\":13195388,\"free\":8357316,\"name\":\"Mem\",\"shared\":1369116,\"total\":32657728,\"used\":11105024},{\"free\":16400380,\"name\":\"Swap\",\"total\":16400380}],\"nics\":[{\"cidrs\":[],\"mac\":\"f8:75:a4:a4:01:6e\",\"mtu\":1500,\"name\":\"enp0s31f6\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[{\"ip-address\":\"10.100.102.12\",\"mask\":24}],\"mac\":\"80:32:53:4f:16:4f\",\"mtu\":1500,\"name\":\"wlp0s20f3\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[{\"ip-address\":\"192.168.39.1\",\"mask\":24}],\"mac\":\"52:54:00:71:50:da\",\"mtu\":1500,\"name\":\"virbr1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"8e:59:a1:a9:14:23\",\"mtu\":1500,\"name\":\"virbr1-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"ip-address\":\"192.168.122.1\",\"mask\":24}],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"52:54:00:bc:9b:3f\",\"mtu\":1500,\"name\":\"virbr0-nic\",\"state\":\"BROADCAST,MULTICAST\"},{\"cidrs\":[{\"ip-address\":\"172.17.0.1\",\"mask\":16}],\"mac\":\"02:42:aa:59:3a:d3\",\"mtu\":1500,\"name\":\"docker0\",\"state\":\"NO-CARRIER,BROADCAST,MULTICAST,UP\"},{\"cidrs\":[],\"mac\":\"fe:9b:ea:d0:f5:70\",\"mtu\":1500,\"name\":\"vnet0\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"},{\"cidrs\":[],\"mac\":\"fe:16:a0:ea:b3:0b\",\"mtu\":1500,\"name\":\"vnet1\",\"state\":\"BROADCAST,MULTICAST,UP,LOWER_UP\"}]}"

		_, err := bmclient.Inventory.PostStepReply(ctx, &inventory.PostStepReplyParams{
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

		_, err = bmclient.Inventory.PostStepReply(ctx, &inventory.PostStepReplyParams{
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

	It("disable enable", func() {
		host := registerHost(clusterID)
		_, err := bmclient.Inventory.DisableHost(ctx, &inventory.DisableHostParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		host = getHost(clusterID, *host.ID)
		Expect(*host.Status).Should(Equal("disabled"))
		Expect(len(getNextSteps(clusterID, *host.ID))).Should(Equal(0))

		_, err = bmclient.Inventory.EnableHost(ctx, &inventory.EnableHostParams{
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
		_, err := bmclient.Inventory.SetDebugStep(ctx, &inventory.SetDebugStepParams{
			ClusterID: clusterID,
			HostID:    *host1.HostID,
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

		_, err = bmclient.Inventory.PostStepReply(ctx, &inventory.PostStepReplyParams{
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

	It("register same host id", func() {
		hostID := strToUUID(uuid.New().String())
		// register to cluster1
		_, err := bmclient.Inventory.RegisterHost(context.Background(), &inventory.RegisterHostParams{
			ClusterID: clusterID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cluster2, err := bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name: swag.String("another cluster"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// register to cluster2
		_, err = bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			ClusterID: *cluster2.GetPayload().ID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// successfully get from both clusters
		_ = getHost(clusterID, *hostID)
		_ = getHost(*cluster2.GetPayload().ID, *hostID)

		_, err = bmclient.Inventory.DeregisterHost(ctx, &inventory.DeregisterHostParams{
			ClusterID: clusterID,
			HostID:    *hostID,
		})
		Expect(err).NotTo(HaveOccurred())
		h := getHost(*cluster2.GetPayload().ID, *hostID)

		// register again to cluster 2 and expect it to be in discovery status
		Expect(db.Model(h).Update("status", "known").Error).NotTo(HaveOccurred())
		h = getHost(*cluster2.GetPayload().ID, *hostID)
		Expect(swag.StringValue(h.Status)).Should(Equal("known"))
		_, err = bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
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
	steps, err := bmclient.Inventory.GetNextSteps(context.Background(), &inventory.GetNextStepsParams{
		ClusterID: clusterID,
		HostID:    hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return steps.GetPayload()
}
