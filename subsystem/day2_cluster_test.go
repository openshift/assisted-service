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
	"github.com/openshift/assisted-service/internal/common"
	hostpkg "github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

const (
	statusInfoAddingHosts = "cluster is adding hosts to existing OCP cluster"
)

var _ = Describe("Day2 v2 cluster tests", func() {
	ctx := context.Background()
	var cluster *installer.V2ImportClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID *strfmt.UUID
	var err error

	BeforeEach(func() {
		openshiftClusterID := strfmt.UUID(uuid.New().String())

		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
			NewImportClusterParams: &models.ImportClusterParams{
				Name:               swag.String("test-cluster"),
				OpenshiftVersion:   openshiftVersion,
				APIVipDnsname:      swag.String("api.test-cluster.example.com"),
				OpenshiftClusterID: &openshiftClusterID,
			},
		})

		By(fmt.Sprintf("clusterID is %s", *cluster.GetPayload().ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("adding-hosts"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(statusInfoAddingHosts))
		Expect(swag.StringValue(&cluster.GetPayload().OpenshiftVersion)).Should(BeEmpty())
		Expect(swag.StringValue(&cluster.GetPayload().OcpReleaseImage)).Should(BeEmpty())
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				PullSecret: swag.String(pullSecret),
			},
			ClusterID: *cluster.GetPayload().ID,
		})
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
	})

	It("cluster CRUD", func() {
		_ = &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		Expect(err).NotTo(HaveOccurred())
		getReply, err1 := utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err1).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		getReply, err = utils_test.TestContext.AgentBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err2 := utils_test.TestContext.UserBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
		Expect(err2).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = utils_test.TestContext.UserBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Day2 cluster tests", func() {
	ctx := context.Background()
	var cluster *installer.V2ImportClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID strfmt.UUID
	var err error

	BeforeEach(func() {
		openshiftClusterID := strfmt.UUID(uuid.New().String())
		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
			NewImportClusterParams: &models.ImportClusterParams{
				Name:               swag.String("test-cluster"),
				APIVipDnsname:      swag.String("api.test-cluster.example.com"),
				OpenshiftVersion:   openshiftVersion,
				OpenshiftClusterID: &openshiftClusterID,
			},
		})
		cluster.Payload.OpenshiftVersion = openshiftVersion

		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("clusterID is %s", *cluster.GetPayload().ID))

		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("adding-hosts"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(statusInfoAddingHosts))
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				PullSecret: swag.String(pullSecret),
			},
			ClusterID: *cluster.GetPayload().ID,
		})
		Expect(err).NotTo(HaveOccurred())

		res, err1 := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: openshiftVersion,
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
				ImageType:        models.ImageTypeFullIso,
				ClusterID:        cluster.GetPayload().ID,
			},
		})

		Expect(err1).NotTo(HaveOccurred())
		infraEnvID = *res.GetPayload().ID
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("cluster update hostname", func() {
		host1 := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host2 := &utils_test.TestContext.RegisterHost(infraEnvID).Host

		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostID:     *host1.ID,
			InfraEnvID: infraEnvID,
			HostUpdateParams: &models.HostUpdateParams{
				HostName: swag.String("host1newname"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostID:     *host2.ID,
			InfraEnvID: infraEnvID,
			HostUpdateParams: &models.HostUpdateParams{
				HostName: swag.String("host2newname"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		h := utils_test.TestContext.GetHostV2(infraEnvID, *host1.ID)
		Expect(h.RequestedHostname).Should(Equal("host1newname"))
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host2.ID)
		Expect(h.RequestedHostname).Should(Equal("host2newname"))
	})

	It("cluster update machineConfigPool", func() {
		host1 := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host2 := &utils_test.TestContext.RegisterHost(infraEnvID).Host

		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostID:     *host1.ID,
			InfraEnvID: infraEnvID,
			HostUpdateParams: &models.HostUpdateParams{
				MachineConfigPoolName: swag.String("host1newpool"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostID:     *host2.ID,
			InfraEnvID: infraEnvID,
			HostUpdateParams: &models.HostUpdateParams{
				MachineConfigPoolName: swag.String("host2newpool"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		h := utils_test.TestContext.GetHostV2(infraEnvID, *host1.ID)
		Expect(h.MachineConfigPoolName).Should(Equal("host1newpool"))
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host2.ID)
		Expect(h.MachineConfigPoolName).Should(Equal("host2newpool"))
	})

	It("check host states - one node", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)

		By("checking discovery state")
		Expect(*h.Status).Should(Equal("discovering"))
		steps := utils_test.TestContext.GetNextSteps(infraEnvID, *host.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory})

		By("checking insufficient state state - one host, no connectivity check")
		ips := hostutil.GenerateIPv4Addresses(2, utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "h1host", ips[0])
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		steps = utils_test.TestContext.GetNextSteps(infraEnvID, *host.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck})

		By("checking known state state - one host, ignition will come from host")
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
	})

	It("check host states - two nodes", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h1 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		host = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h2 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, utils_test.DefaultCIDRv4)
		By("checking discovery state")
		Expect(*h1.Status).Should(Equal("discovering"))
		steps := utils_test.TestContext.GetNextSteps(infraEnvID, *h1.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory})

		By("checking discovery state host2")
		Expect(*h2.Status).Should(Equal("discovering"))
		steps = utils_test.TestContext.GetNextSteps(infraEnvID, *h2.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory})

		By("checking insufficient state state host2 ")
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h2, "h2host", ips[1])
		utils_test.TestContext.GenerateDomainResolution(ctx, h2, "test-cluster", "")
		utils_test.TestContext.GenerateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h2)
		steps = utils_test.TestContext.GetNextSteps(infraEnvID, *h2.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck})

		By("checking insufficient state state")
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1host", ips[0])
		utils_test.TestContext.GenerateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		utils_test.TestContext.GenerateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h1)
		steps = utils_test.TestContext.GetNextSteps(infraEnvID, *h1.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInventory, models.StepTypeAPIVipConnectivityCheck, models.StepTypeConnectivityCheck})

		By("checking known state state")
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h1, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h1, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h1)
	})

	It("check installation - one node", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageStartingInstallation)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageRebooting)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 2 nodes", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h1 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		host = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h2 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		utils_test.TestContext.GenerateDomainResolution(ctx, h1, "test-cluster", "")
		utils_test.TestContext.GenerateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h1)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h1, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h1, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h1)

		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		utils_test.TestContext.GenerateDomainResolution(ctx, h2, "test-cluster", "")
		utils_test.TestContext.GenerateConnectivityCheckPostStepReply(ctx, h2, ips[0], true)
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h2)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h2, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h2, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h2)

		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h1.ID})
		Expect(err).NotTo(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h2.ID})
		Expect(err).NotTo(HaveOccurred())
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing"))
		Expect(h1.Role).Should(Equal(models.HostRoleWorker))
		h2 = utils_test.TestContext.GetHostV2(infraEnvID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing"))
		Expect(h2.Role).Should(Equal(models.HostRoleWorker))

		utils_test.TestContext.UpdateProgress(*h1.ID, infraEnvID, models.HostStageStartingInstallation)
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(*h1.Status).Should(Equal("installing-in-progress"))
		utils_test.TestContext.UpdateProgress(*h2.ID, infraEnvID, models.HostStageStartingInstallation)
		h2 = utils_test.TestContext.GetHostV2(infraEnvID, *h2.ID)
		Expect(*h2.Status).Should(Equal("installing-in-progress"))

		utils_test.TestContext.UpdateProgress(*h1.ID, infraEnvID, models.HostStageRebooting)
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(*h1.Status).Should(Equal("added-to-existing-cluster"))
		utils_test.TestContext.UpdateProgress(*h2.ID, infraEnvID, models.HostStageRebooting)
		h2 = utils_test.TestContext.GetHostV2(infraEnvID, *h2.ID)
		Expect(*h2.Status).Should(Equal("added-to-existing-cluster"))

		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - 0 nodes", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h1 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		host = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h2 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "hostname1", ips[0])
		utils_test.TestContext.GenerateDomainResolution(ctx, h1, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h1)

		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h2, "hostname2", ips[1])
		utils_test.TestContext.GenerateDomainResolution(ctx, h2, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h2)

		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h1.ID})
		Expect(err).To(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h2.ID})
		Expect(err).To(HaveOccurred())

		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))
		Expect(h1.Role).Should(Equal(models.HostRoleWorker))
		h2 = utils_test.TestContext.GetHostV2(infraEnvID, *h2.ID)
		Expect(*h2.Status).Should(Equal("insufficient"))
		Expect(h2.Role).Should(Equal(models.HostRoleWorker))

		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
	})

	It("check installation - install specific node", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageStartingInstallation)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageRebooting)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
	})

	It("check installation - node registers after reboot", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		steps := utils_test.TestContext.GetNextSteps(infraEnvID, *h.ID)
		utils_test.AreStepsInList(steps, []models.StepType{models.StepTypeInstall})
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageStartingInstallation)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageRebooting)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_ = utils_test.TestContext.RegisterHostByUUID(infraEnvID, *h.ID)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-pending-user-action"))
	})

	It("reset node after failed installation", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageStartingInstallation)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageRebooting)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("added-to-existing-cluster"))
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).Should(Equal("adding-hosts"))
		_, err = utils_test.TestContext.UserBMClient.Installer.V2ResetHost(ctx, &installer.V2ResetHostParams{InfraEnvID: infraEnvID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &utils_test.TestContext.RegisterHostByUUID(infraEnvID, *host.ID).Host
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node during failed installation", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		utils_test.TestContext.UpdateProgress(*h.ID, infraEnvID, models.HostStageStartingInstallation)
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing-in-progress"))
		_, err = utils_test.TestContext.UserBMClient.Installer.V2ResetHost(ctx, &installer.V2ResetHostParams{InfraEnvID: infraEnvID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &utils_test.TestContext.RegisterHostByUUID(infraEnvID, *host.ID).Host
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})

	It("reset node failed install command", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h, "hostname", utils_test.DefaultCIDRv4)
		utils_test.TestContext.GenerateDomainResolution(ctx, h, "test-cluster", "")
		waitForHostStateV2(ctx, "insufficient", 60*time.Second, h)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h, cluster.Payload, true)
		waitForHostStateV2(ctx, "known", 60*time.Second, h)
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *h.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("installing"))
		Expect(h.Role).Should(Equal(models.HostRoleWorker))
		// post failure to execute the install command
		_, err = utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: infraEnvID,
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
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("error"))
		_, err = utils_test.TestContext.UserBMClient.Installer.V2ResetHost(ctx, &installer.V2ResetHostParams{InfraEnvID: infraEnvID, HostID: *host.ID})
		Expect(err).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("resetting-pending-user-action"))
		host = &utils_test.TestContext.RegisterHostByUUID(infraEnvID, *host.ID).Host
		h = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*h.Status).Should(Equal("discovering"))
	})
})

var _ = Describe("Day2 cluster with bind/unbind hosts", func() {
	ctx := context.Background()
	var cluster *installer.V2ImportClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID strfmt.UUID
	var err error

	BeforeEach(func() {
		openshiftClusterID := strfmt.UUID(uuid.New().String())
		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
			NewImportClusterParams: &models.ImportClusterParams{
				Name:               swag.String("test-cluster"),
				APIVipDnsname:      swag.String("api.test-cluster.example.com"),
				OpenshiftClusterID: &openshiftClusterID,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID

		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				PullSecret: swag.String(pullSecret),
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		infraEnv, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: openshiftVersion,
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
				ImageType:        models.ImageTypeFullIso,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		infraEnvID = *infraEnv.GetPayload().ID
	})

	It("check host states with binding - two nodes", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h1 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		host = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		h2 := utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		ips := hostutil.GenerateIPv4Addresses(2, utils_test.DefaultCIDRv4)

		By("hosts in state discovering-unbound")
		Expect(*h1.Status).Should(Equal(models.HostStatusDiscoveringUnbound))
		Expect(*h2.Status).Should(Equal(models.HostStatusDiscoveringUnbound))

		By("host h1 become known-unbound after inventory reply")
		utils_test.TestContext.GenerateGetNextStepsWithTimestamp(ctx, h1, time.Now().Unix())
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, 60*time.Second, h1)

		By("bind host h1 and re-register - host become insufficient")
		utils_test.TestContext.BindHost(infraEnvID, *h1.ID, clusterID)
		waitForHostStateV2(ctx, models.HostStatusBinding, 60*time.Second, h1)
		h1 = &utils_test.TestContext.RegisterHostByUUID(infraEnvID, *h1.ID).Host
		utils_test.TestContext.GenerateGetNextStepsWithTimestamp(ctx, h1, time.Now().Unix())
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusInsufficient, 60*time.Second, h1)

		By("add connectivity - host h1 becomes known")
		utils_test.TestContext.GenerateDomainResolution(ctx, h1, "test-cluster", "")
		utils_test.TestContext.GenerateConnectivityCheckPostStepReply(ctx, h1, ips[1], true)
		utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, h1, *common.TestDomainNameResolutionsSuccess)
		utils_test.TestContext.GenerateApiVipPostStepReply(ctx, h1, cluster.Payload, true)
		waitForHostStateV2(ctx, models.HostStatusKnown, 60*time.Second, h1)
	})
})

var _ = Describe("Installation progress", func() {
	var (
		ctx        = context.Background()
		c          *models.Cluster
		infraEnvID strfmt.UUID
	)

	It("Test installation progress - day2", func() {

		By("register cluster", func() {

			// register cluster
			openshiftClusterID := strfmt.UUID(uuid.New().String())

			importClusterReply, err := utils_test.TestContext.UserBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
				NewImportClusterParams: &models.ImportClusterParams{
					Name:               swag.String("day2-cluster"),
					APIVipDnsname:      swag.String("api.test-cluster.example.com"),
					OpenshiftClusterID: &openshiftClusterID,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = importClusterReply.GetPayload()
			c.OpenshiftVersion = openshiftVersion

			_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					PullSecret: swag.String(pullSecret),
				},
				ClusterID: *c.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			res, err1 := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeFullIso,
					ClusterID:        c.ID,
				},
			})

			Expect(err1).NotTo(HaveOccurred())
			infraEnvID = *res.GetPayload().ID

			// register host to be used by the test as day2 host
			// day2 host is now initialized as a worker
			utils_test.TestContext.RegisterHost(infraEnvID)
			c = utils_test.TestContext.GetCluster(*c.ID)

			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("install hosts", func() {

			utils_test.TestContext.GenerateEssentialHostSteps(ctx, c.Hosts[0], "hostname", utils_test.DefaultCIDRv4)
			utils_test.TestContext.GenerateDomainResolution(ctx, c.Hosts[0], "day2-cluster", "")
			utils_test.TestContext.GenerateApiVipPostStepReply(ctx, c.Hosts[0], nil, true)
			utils_test.TestContext.GenerateDomainNameResolutionReply(ctx, c.Hosts[0], *common.TestDomainNameResolutionsSuccess)
			utils_test.TestContext.GenerateApiVipPostStepReply(ctx, c.Hosts[0], c, true)
			waitForHostStateV2(ctx, "known", 60*time.Second, c.Hosts[0])

			_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallHost(ctx, &installer.V2InstallHostParams{InfraEnvID: infraEnvID, HostID: *c.Hosts[0].ID})
			Expect(err).NotTo(HaveOccurred())

			c = utils_test.TestContext.GetCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing"))
			Expect(c.Hosts[0].Role).Should(Equal(models.HostRoleWorker))

			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(0)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 1st report", func() {

			utils_test.TestContext.UpdateProgress(*c.Hosts[0].ID, infraEnvID, models.HostStageStartingInstallation)
			c = utils_test.TestContext.GetCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal("installing-in-progress"))

			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(20)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 2nd report", func() {

			utils_test.TestContext.UpdateProgress(*c.Hosts[0].ID, infraEnvID, models.HostStageInstalling)
			c = utils_test.TestContext.GetCluster(*c.ID)
			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(40)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - 3rd report", func() {

			utils_test.TestContext.UpdateProgress(*c.Hosts[0].ID, infraEnvID, models.HostStageWritingImageToDisk)
			c = utils_test.TestContext.GetCluster(*c.ID)
			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(60)))
			expectProgressToBe(c, 0, 0, 0)
		})

		By("report hosts' progress - last report", func() {

			utils_test.TestContext.UpdateProgress(*c.Hosts[0].ID, infraEnvID, models.HostStageRebooting)
			c = utils_test.TestContext.GetCluster(*c.ID)
			Expect(*c.Hosts[0].Status).Should(Equal(models.HostStatusAddedToExistingCluster))
			Expect(c.Hosts[0].ProgressStages).To(Equal(hostpkg.WorkerStages[:5]))
			Expect(c.Hosts[0].Progress.InstallationPercentage).To(Equal(int64(100)))
			expectProgressToBe(c, 0, 0, 0)
		})
	})
})
