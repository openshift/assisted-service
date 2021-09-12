package subsystem

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

const (
	statusInfoAddingHosts = "cluster is adding hosts to existing OCP cluster"
)

var _ = Describe("Day2 cluster tests", func() {
	ctx := context.Background()
	var cluster *installer.RegisterAddHostsClusterCreated
	var clusterID strfmt.UUID
	var err error

	BeforeEach(func() {
		cluster, err = userBMClient.Installer.RegisterAddHostsCluster(ctx, &installer.RegisterAddHostsClusterParams{
			NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				APIVipDnsname:    swag.String("api_vip_dnsname"),
				ID:               strToUUID(uuid.New().String()),
			},
		})

		By(fmt.Sprintf("clusterID is %s", *cluster.GetPayload().ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("adding-hosts"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(statusInfoAddingHosts))
		Expect(swag.StringValue(&cluster.GetPayload().OpenshiftVersion)).Should(ContainSubstring(openshiftVersion))
		Expect(swag.StringValue(&cluster.GetPayload().OcpReleaseImage)).Should(ContainSubstring(openshiftVersion))
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				PullSecret: swag.String(pullSecret),
			},
			ClusterID: *cluster.GetPayload().ID,
		})
		Expect(err).NotTo(HaveOccurred())
		// in order to simulate infra env generation
		generateClusterISO(*cluster.GetPayload().ID, models.ImageTypeMinimalIso)
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	AfterEach(func() {
		clearDB()
	})

	It("cluster CRUD", func() {
		_ = &registerHost(clusterID).Host
		Expect(err).NotTo(HaveOccurred())
		getReply, err1 := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err1).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		getReply, err = agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err2 := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err2).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update hostname", func() {
		host1 := &registerHost(clusterID).Host
		host2 := &registerHost(clusterID).Host

		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
					{ID: *host1.ID, Hostname: "host1newname"},
					{ID: *host2.ID, Hostname: "host2newname"},
				},
			},
			ClusterID: clusterID,
		})

		h := getHost(clusterID, *host1.ID)
		Expect(h.RequestedHostname).Should(Equal("host1newname"))
		h = getHost(clusterID, *host2.ID)
		Expect(h.RequestedHostname).Should(Equal("host2newname"))
	})

	It("cluster update machineConfigPool", func() {
		host1 := &registerHost(clusterID).Host
		host2 := &registerHost(clusterID).Host

		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				HostsMachineConfigPoolNames: []*models.ClusterUpdateParamsHostsMachineConfigPoolNamesItems0{
					{ID: *host1.ID, MachineConfigPoolName: "host1newpool"},
					{ID: *host2.ID, MachineConfigPoolName: "host2newpool"},
				},
			},
			ClusterID: clusterID,
		})

		h := getHost(clusterID, *host1.ID)
		Expect(h.MachineConfigPoolName).Should(Equal("host1newpool"))
		h = getHost(clusterID, *host2.ID)
		Expect(h.MachineConfigPoolName).Should(Equal("host2newpool"))
	})

	It("check host states - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)

		By("checking discovery state")
		Expect(*h.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state - one host, no connectivity check")
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h, "h1host", ips[0])
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking known state state - one host, no connectivity check")
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck}, 1)
	})

	It("check host states - two nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		By("checking discovery state")
		Expect(*h1.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking discovery state host2")
		Expect(*h2.Status).Should(Equal("discovering"))
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state host2 ")
		generateEssentialHostSteps(ctx, h2, "h2host", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking insufficient state state")
		generateEssentialHostSteps(ctx, h1, "h1host", ips[0])
		generateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		generateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 3)

		By("checking known state state")
		generateApiVipPostStepReply(ctx, h1, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h1)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 2)
	})

	It("check installation - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
	})

	It("check installation - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 2 nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		generateDomainResolution(ctx, h1, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)
		generateApiVipPostStepReply(ctx, h1, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h1)

		generateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)
		generateApiVipPostStepReply(ctx, h2, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h2)

		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing"))
		Expect(h1.Role).Should(Equal(models.HostRoleWorker))
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing"))
		Expect(h2.Role).Should(Equal(models.HostRoleWorker))

		updateProgress(*h1.ID, clusterID, models.HostStageStartingInstallation)
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h2.ID, clusterID, models.HostStageStartingInstallation)
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing-in-progress"))

		updateProgress(*h1.ID, clusterID, models.HostStageRebooting)
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("added-to-existing-cluster"))
		updateProgress(*h2.ID, clusterID, models.HostStageRebooting)
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("added-to-existing-cluster"))

		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 0 nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		generateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)

		generateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)

		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))
		Expect(h1.Role).Should(Equal(models.HostRoleAutoAssign))
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("insufficient"))
		Expect(h2.Role).Should(Equal(models.HostRoleAutoAssign))

		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - install specific node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHost(ctx, &installer.InstallHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
	})

	It("check installation - node registers after reboot", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		steps := getNextSteps(clusterID, *h.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInstall}, 1)
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_ = registerHostByUUID(clusterID, *h.ID)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-pending-user-action"))
	})

	It("reset node after failed installation", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node during failed installation", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node failed install command", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		// post failure to execute the install command
		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: clusterID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: bminventory.ContainerAlreadyRunningExitCode,
				StepType: models.StepTypeInstall,
				Output:   "blabla",
				Error:    "Some random error",
				StepID:   string(models.StepTypeInstall),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("error"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

})

var _ = Describe("[V2UpdateCluster] Day2 cluster tests", func() {
	ctx := context.Background()
	var cluster *installer.RegisterAddHostsClusterCreated
	var clusterID strfmt.UUID
	var err error

	BeforeEach(func() {
		cluster, err = userBMClient.Installer.RegisterAddHostsCluster(ctx, &installer.RegisterAddHostsClusterParams{
			NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				APIVipDnsname:    swag.String("api_vip_dnsname"),
				ID:               strToUUID(uuid.New().String()),
			},
		})

		By(fmt.Sprintf("clusterID is %s", *cluster.GetPayload().ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("adding-hosts"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(statusInfoAddingHosts))
		Expect(swag.StringValue(&cluster.GetPayload().OpenshiftVersion)).Should(ContainSubstring(openshiftVersion))
		Expect(swag.StringValue(&cluster.GetPayload().OcpReleaseImage)).Should(ContainSubstring(openshiftVersion))
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				PullSecret: swag.String(pullSecret),
			},
			ClusterID: *cluster.GetPayload().ID,
		})
		Expect(err).NotTo(HaveOccurred())
		// in order to simulate infra env generation
		generateClusterISO(*cluster.GetPayload().ID, models.ImageTypeMinimalIso)
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	AfterEach(func() {
		clearDB()
	})

	It("cluster CRUD", func() {
		_ = &registerHost(clusterID).Host
		Expect(err).NotTo(HaveOccurred())
		getReply, err1 := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err1).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		getReply, err = agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err2 := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err2).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("check host states - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)

		By("checking discovery state")
		Expect(*h.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state - one host, no connectivity check")
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h, "h1host", ips[0])
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking known state state - one host, no connectivity check")
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck}, 1)
	})

	It("check host states - two nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		By("checking discovery state")
		Expect(*h1.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking discovery state host2")
		Expect(*h2.Status).Should(Equal("discovering"))
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state host2 ")
		generateEssentialHostSteps(ctx, h2, "h2host", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking insufficient state state")
		generateEssentialHostSteps(ctx, h1, "h1host", ips[0])
		generateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		generateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 3)

		By("checking known state state")
		generateApiVipPostStepReply(ctx, h1, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h1)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 2)
	})

	It("check installation - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
	})

	It("check installation - one node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 2 nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		generateDomainResolution(ctx, h1, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)
		generateApiVipPostStepReply(ctx, h1, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h1)

		generateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		generateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)
		generateApiVipPostStepReply(ctx, h2, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h2)

		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing"))
		Expect(h1.Role).Should(Equal(models.HostRoleWorker))
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing"))
		Expect(h2.Role).Should(Equal(models.HostRoleWorker))

		updateProgress(*h1.ID, clusterID, models.HostStageStartingInstallation)
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h2.ID, clusterID, models.HostStageStartingInstallation)
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing-in-progress"))

		updateProgress(*h1.ID, clusterID, models.HostStageRebooting)
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("added-to-existing-cluster"))
		updateProgress(*h2.ID, clusterID, models.HostStageRebooting)
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("added-to-existing-cluster"))

		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 0 nodes", func() {
		host := &registerHost(clusterID).Host
		h1 := getHost(clusterID, *host.ID)
		host = &registerHost(clusterID).Host
		h2 := getHost(clusterID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
		generateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		generateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h1)

		generateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		generateDomainResolution(ctx, h2, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h2)

		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))
		Expect(h1.Role).Should(Equal(models.HostRoleAutoAssign))
		h2 = getHost(clusterID, *h2.ID)
		Expect(*h2.Status).Should(Equal("insufficient"))
		Expect(h2.Role).Should(Equal(models.HostRoleAutoAssign))

		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - install specific node", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHost(ctx, &installer.InstallHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
	})

	It("check installation - node registers after reboot", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		steps := getNextSteps(clusterID, *h.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInstall}, 1)
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_ = registerHostByUUID(clusterID, *h.ID)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-pending-user-action"))
	})

	It("reset node after failed installation", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		updateProgress(*h.ID, clusterID, models.HostStageRebooting)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := getCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node during failed installation", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		updateProgress(*h.ID, clusterID, models.HostStageStartingInstallation)
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node failed install command", func() {
		host := &registerHost(clusterID).Host
		h := getHost(clusterID, *host.ID)
		generateEssentialHostSteps(ctx, h, "hostname", defaultCIDRv4)
		generateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostState(ctx, clusterID, "insufficient", 60*time.Second, h)
		generateApiVipPostStepReply(ctx, h, true)
		waitForHostState(ctx, clusterID, "known", 60*time.Second, h)
		_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		// post failure to execute the install command
		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: clusterID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: bminventory.ContainerAlreadyRunningExitCode,
				StepType: models.StepTypeInstall,
				Output:   "blabla",
				Error:    "Some random error",
				StepID:   string(models.StepTypeInstall),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("error"))
		_, err = userBMClient.Installer.ResetHost(ctx, &installer.ResetHostParams{ClusterID: clusterID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &registerHostByUUID(clusterID, *host.ID).Host
		h = getHost(clusterID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

})

var _ = Describe("Installation progress", func() {
	var (
		ctx = context.Background()
		c   *models.Cluster
	)

	AfterEach(func() {
		clearDB()
	})

	It("Test installation progress - day2", func() {

		By("register cluster", func() {

			// register cluster

			registerAddHostsClusterReply, err := userBMClient.Installer.RegisterAddHostsCluster(ctx, &installer.RegisterAddHostsClusterParams{
				NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
					Name:             swag.String("day2-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					APIVipDnsname:    swag.String("api_vip_dnsname"),
					ID:               strToUUID(uuid.New().String()),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = registerAddHostsClusterReply.GetPayload()

			_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					PullSecret: swag.String(pullSecret),
				},
				ClusterID: *c.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			// in order to simulate infra env generation
			generateClusterISO(*c.ID, models.ImageTypeMinimalIso)

			// add day2 host

			registerHost(*c.ID)
			c = getCluster(*c.ID)

			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("install hosts", func() {

			generateEssentialHostSteps(ctx, c.Hosts[0], "hostname", defaultCIDRv4)
			generateDomainResolution(ctx, c.Hosts[0], "day2-cluster", "")
			generateApiVipPostStepReply(ctx, c.Hosts[0], true)
			waitForHostState(ctx, *c.ID, "known", 60*time.Second, c.Hosts[0])

			_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: *c.ID})
			Expect(err).NotTo(HaveOccurred())

			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing"))
			Expect(c.Hosts[0].Role).Should(Equal(models.HostRoleWorker))

			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 1st report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageStartingInstallation)
			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing-in-progress"))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(20)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 2nd report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageInstalling)
			c = getCluster(*c.ID)
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(40)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 3rd report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageWritingImageToDisk)
			c = getCluster(*c.ID)
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(60)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - last report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageRebooting)
			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal(models.HostStatusAddedToExistingCluster))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(100)))
			expectProgressToBe(c, 0, 0, 0)
		})
	})

	It("[V2UpdateCluster] Test installation progress - day2", func() {

		By("register cluster", func() {

			// register cluster

			registerAddHostsClusterReply, err := userBMClient.Installer.RegisterAddHostsCluster(ctx, &installer.RegisterAddHostsClusterParams{
				NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
					Name:             swag.String("day2-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					APIVipDnsname:    swag.String("api_vip_dnsname"),
					ID:               strToUUID(uuid.New().String()),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = registerAddHostsClusterReply.GetPayload()

			_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					PullSecret: swag.String(pullSecret),
				},
				ClusterID: *c.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			// in order to simulate infra env generation
			generateClusterISO(*c.ID, models.ImageTypeMinimalIso)

			// add day2 host

			registerHost(*c.ID)
			c = getCluster(*c.ID)

			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
		})

		By("install hosts", func() {

			generateEssentialHostSteps(ctx, c.Hosts[0], "hostname", defaultCIDRv4)
			generateDomainResolution(ctx, c.Hosts[0], "day2-cluster", "")
			generateApiVipPostStepReply(ctx, c.Hosts[0], true)
			waitForHostState(ctx, *c.ID, "known", 60*time.Second, c.Hosts[0])

			_, err := userBMClient.Installer.InstallHosts(ctx, &installer.InstallHostsParams{ClusterID: *c.ID})
			Expect(err).NotTo(HaveOccurred())

			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing"))
			Expect(c.Hosts[0].Role).Should(Equal(models.HostRoleWorker))

			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
		})

		By("report hosts' progress - 1st report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageStartingInstallation)
			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing-in-progress"))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(20)))
		})

		By("report hosts' progress - 2nd report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageInstalling)
			c = getCluster(*c.ID)
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(40)))
		})

		By("report hosts' progress - 3rd report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageWritingImageToDisk)
			c = getCluster(*c.ID)
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(60)))
		})

		By("report hosts' progress - last report", func() {

			updateProgress(*c.Hosts[0].ID, *c.ID, models.HostStageRebooting)
			c = getCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal(models.HostStatusAddedToExistingCluster))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(100)))
		})
	})
})
