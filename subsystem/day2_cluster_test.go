package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/openshift/assisted-service/client/installer"
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
				OpenshiftVersion: swag.String("4.6"),
				APIVipDnsname:    swag.String("api_vip_dnsname"),
				ID:               strToUUID(uuid.New().String()),
			},
		})
		By(fmt.Sprintf("clusterID is %s", *cluster.GetPayload().ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("adding-hosts"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(statusInfoAddingHosts))
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	AfterEach(func() {
		clearDB()
	})

	generateApiVipPostStepReply := func(h *models.Host, success bool) {
		checkVipApiResponse := models.APIVipConnectivityResponse{
			IsSuccess: success,
		}
		bytes, jsonErr := json.Marshal(checkVipApiResponse)
		Expect(jsonErr).NotTo(HaveOccurred())
		_, err = agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				StepType: models.StepTypeAPIVipConnectivityCheck,
				Output:   string(bytes),
				StepID:   "apivip-connectivity-check-step",
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}

	It("cluster CRUD", func() {
		_ = registerHost(clusterID)
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
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)

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

	It("check host states - one node", func() {
		host := registerHost(clusterID)
		h := getHost(clusterID, *host.ID)

		By("checking discovery state")
		Expect(*h.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state - one host, no conenctivity check")
		generateHWPostStepReply(ctx, h, validHwInfo, "h1host")
		waitForHostState(ctx, clusterID, *h.ID, "insufficient", 60*time.Second)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking known state state - one host, no connectivity check")
		generateApiVipPostStepReply(h, true)
		waitForHostState(ctx, clusterID, *h.ID, "known", 60*time.Second)
		steps = getNextSteps(clusterID, *host.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck}, 1)
	})

	It("check host states - two nodes", func() {
		host := registerHost(clusterID)
		h1 := getHost(clusterID, *host.ID)
		host = registerHost(clusterID)
		h2 := getHost(clusterID, *host.ID)

		By("checking discovery state")
		Expect(*h1.Status).Should(Equal("discovering"))
		steps := getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking discovery state host2")
		Expect(*h2.Status).Should(Equal("discovering"))
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory}, 1)

		By("checking insufficient state state host2 ")
		generateHWPostStepReply(ctx, h2, validHwInfo, "h2host")
		waitForHostState(ctx, clusterID, *h2.ID, "insufficient", 60*time.Second)
		steps = getNextSteps(clusterID, *h2.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck}, 2)

		By("checking insufficient state state")
		generateHWPostStepReply(ctx, h1, validHwInfo, "h1host")
		waitForHostState(ctx, clusterID, *h1.ID, "insufficient", 60*time.Second)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 3)

		By("checking known state state")
		generateApiVipPostStepReply(h1, true)
		waitForHostState(ctx, clusterID, *h1.ID, "known", 60*time.Second)
		steps = getNextSteps(clusterID, *h1.ID)
		checkStepsInList(steps, []models.StepType{models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck}, 2)
	})

	It("check installation - one node", func() {
		host := registerHost(clusterID)
		h := getHost(clusterID, *host.ID)
		generateHWPostStepReply(ctx, h, validHwInfo, "hostname")
		waitForHostState(ctx, clusterID, *h.ID, "insufficient", 60*time.Second)
		generateApiVipPostStepReply(h, true)
		waitForHostState(ctx, clusterID, *h.ID, "known", 60*time.Second)
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
		host := registerHost(clusterID)
		h := getHost(clusterID, *host.ID)
		generateHWPostStepReply(ctx, h, validHwInfo, "hostname")
		waitForHostState(ctx, clusterID, *h.ID, "insufficient", 60*time.Second)
		generateApiVipPostStepReply(h, true)
		waitForHostState(ctx, clusterID, *h.ID, "known", 60*time.Second)
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
		host := registerHost(clusterID)
		h1 := getHost(clusterID, *host.ID)
		host = registerHost(clusterID)
		h2 := getHost(clusterID, *host.ID)

		generateHWPostStepReply(ctx, h1, validHwInfo, "hostname1")
		waitForHostState(ctx, clusterID, *h1.ID, "insufficient", 60*time.Second)
		generateApiVipPostStepReply(h1, true)
		waitForHostState(ctx, clusterID, *h1.ID, "known", 60*time.Second)

		generateHWPostStepReply(ctx, h2, validHwInfo, "hostname2")
		waitForHostState(ctx, clusterID, *h2.ID, "insufficient", 60*time.Second)
		generateApiVipPostStepReply(h2, true)
		waitForHostState(ctx, clusterID, *h2.ID, "known", 60*time.Second)

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
		host := registerHost(clusterID)
		h1 := getHost(clusterID, *host.ID)
		host = registerHost(clusterID)
		h2 := getHost(clusterID, *host.ID)

		generateHWPostStepReply(ctx, h1, validHwInfo, "hostname1")
		waitForHostState(ctx, clusterID, *h1.ID, "insufficient", 60*time.Second)

		generateHWPostStepReply(ctx, h2, validHwInfo, "hostname2")
		waitForHostState(ctx, clusterID, *h2.ID, "insufficient", 60*time.Second)

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

	It("check installation - node registers after reboot", func() {
		host := registerHost(clusterID)
		h := getHost(clusterID, *host.ID)
		generateHWPostStepReply(ctx, h, validHwInfo, "hostname")
		waitForHostState(ctx, clusterID, *h.ID, "insufficient", 60*time.Second)
		generateApiVipPostStepReply(h, true)
		waitForHostState(ctx, clusterID, *h.ID, "known", 60*time.Second)
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
})
