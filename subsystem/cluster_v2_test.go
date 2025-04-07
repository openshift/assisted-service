package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("Cluster UI Settings", func() {
	var (
		clusterId strfmt.UUID
	)
	ctx := context.Background()
	BeforeEach(func() {
		response, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				BaseDNSDomain:    "example.com",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterId = *response.Payload.ID
	})

	It("Should be able to store and retrieve cluster UI settings", func() {
		_, err := utils_test.TestContext.UserBMClient.Installer.V2UpdateClusterUISettings(ctx, &installer.V2UpdateClusterUISettingsParams{
			ClusterID:  clusterId,
			UISettings: "{\"foo\":\"bar\"}",
		})
		Expect(err).ToNot(HaveOccurred())
		By("Should be able to retrieve cluster UI settings", func() {
			response, err := utils_test.TestContext.UserBMClient.Installer.V2GetClusterUISettings(ctx, &installer.V2GetClusterUISettingsParams{
				ClusterID: clusterId,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(response.Payload).To(Equal("{\"foo\":\"bar\"}"))
		})
	})
})

var _ = Describe("[V2ClusterTests]", func() {
	ctx := context.Background()
	var clusterID strfmt.UUID
	var infraEnvID strfmt.UUID
	var boundInfraEnv strfmt.UUID
	var ips []string
	var h1, h2, h3 *models.Host

	BeforeEach(func() {
		clusterReq, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				BaseDNSDomain:    "example.com",
			},
		})

		Expect(err).NotTo(HaveOccurred())
		clusterID = *clusterReq.GetPayload().ID

		//standalone infraEnv
		infraEnv := registerInfraEnv(nil, models.ImageTypeFullIso)
		infraEnvID = *infraEnv.ID

		//bound infraEnv
		infraEnv = registerInfraEnv(&clusterID, models.ImageTypeFullIso)
		boundInfraEnv = *infraEnv.ID

		By("register h2 h3 to cluster via the bound infraEnv")
		ips = hostutil.GenerateIPv4Addresses(3, utils_test.DefaultCIDRv4)
		h2 = utils_test.TestContext.RegisterNode(ctx, boundInfraEnv, "h2", ips[1])
		h3 = utils_test.TestContext.RegisterNode(ctx, boundInfraEnv, "h3", ips[2])
		utils_test.TestContext.V2UpdateVipParams(ctx, clusterID)
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)
	})

	It("Bind/Unbind single host in 3 nodes cluster (standalone InfraEnv)", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with the stand-alone InfraEnv")
		h1 = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)

		//bind the 3rd node and re-register it
		By("bind h1 to cluster")
		utils_test.TestContext.BindHost(infraEnvID, *h1.ID, clusterID)
		waitForHostStateV2(ctx, models.HostStatusBinding, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("register h1 again and define the connectivity to the other hosts")
		h1 = &utils_test.TestContext.RegisterHostByUUID(h1.InfraEnvID, *h1.ID).Host

		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)

		By("verify host name is set")
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(h1.RequestedHostname).To(Equal("h1"))

		By("unbind host and re-register h1 --> cluster return to insufficient")
		utils_test.TestContext.UnbindHost(infraEnvID, *h1.ID)
		h1 = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("verify that the cluster status is updated immediately")
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).To(Equal(models.ClusterStatusInsufficient))

		By("verify that the unbound host still retains its name and disks count")
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(h1.RequestedHostname).To(Equal(("h1")))
		var inventory models.Inventory
		_ = json.Unmarshal([]byte(h1.Inventory), &inventory)
		Expect(len(inventory.Disks)).To(Equal(2))
	})

	It("register single host in 3 nodes cluster (bound InfraEnv)", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with the bound InfraEnv (implicit binding)")
		h1 = &utils_test.TestContext.RegisterHost(boundInfraEnv).Host
		host := utils_test.TestContext.GetHostV2(boundInfraEnv, *h1.ID)
		Expect(host.ClusterID).NotTo(BeNil())

		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)

		By("unbind host should fail since infraEnv is bound to cluster")
		_, err := utils_test.TestContext.UserBMClient.Installer.UnbindHost(ctx, &installer.UnbindHostParams{
			HostID:     *h1.ID,
			InfraEnvID: boundInfraEnv,
		})
		Expect(err).NotTo(BeNil())
	})

	It("Hosts unbinding on cluster delete", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with InfraEnv")
		h1 = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)

		//bind the 3rd node and re-register it
		By("bind h1 to cluster")
		utils_test.TestContext.BindHost(infraEnvID, *h1.ID, clusterID)
		waitForHostStateV2(ctx, models.HostStatusBinding, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("register h1 again and define the connectivity to the other hosts")
		h1 = &utils_test.TestContext.RegisterHostByUUID(h1.InfraEnvID, *h1.ID).Host

		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)

		By("Delete Cluster")
		_, err := utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for unbinding")
		waitForHostStateV2(ctx, models.HostStatusUnbinding, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("Host is unbound")
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(h1.ClusterID).To(BeNil())

		By("Other hosts are deleted")
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h2.ID,
		})

		Expect(err).To(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h3.ID,
		})
		Expect(err).To(HaveOccurred())
	})

	It("Host unbinding pending user action on cluster delete", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with InfraEnv")
		h1 = &utils_test.TestContext.RegisterHost(infraEnvID).Host
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)

		//bind the 3rd node and re-register it
		By("bind h1 to cluster")
		utils_test.TestContext.BindHost(infraEnvID, *h1.ID, clusterID)
		waitForHostStateV2(ctx, models.HostStatusBinding, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("register h1 again and define the connectivity to the other hosts")
		h1 = &utils_test.TestContext.RegisterHostByUUID(h1.InfraEnvID, *h1.ID).Host

		utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)

		By("Start installation")
		_, err := utils_test.TestContext.UserBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1, h2, h3)
		waitForClusterState(context.Background(), clusterID, models.ClusterStatusInstalling,
			3*time.Minute, utils_test.IgnoreStateInfo)

		By("Cancel installation")
		_, err = utils_test.TestContext.UserBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(context.Background(), clusterID, models.ClusterStatusCancelled,
			3*time.Minute, utils_test.IgnoreStateInfo)

		By("Delete Cluster")
		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		By("Wait for unbinding Pending User Action")
		waitForHostStateV2(ctx, models.HostStatusUnbindingPendingUserAction, utils_test.DefaultWaitForHostStateTimeout, h1)

		By("Host is unbound")
		h1 = utils_test.TestContext.GetHostV2(infraEnvID, *h1.ID)
		Expect(h1.ClusterID).To(BeNil())

		By("Other hosts are deleted")
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h2.ID,
		})

		Expect(err).To(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h3.ID,
		})
		Expect(err).To(HaveOccurred())

		By("register h1 again")
		h1 = &utils_test.TestContext.RegisterHostByUUID(h1.InfraEnvID, *h1.ID).Host
		utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)
	})

	It("Cluster validations are run after host update", func() {
		By("register 3 nodes and check that the cluster is ready")
		h1 = utils_test.TestContext.RegisterNode(ctx, boundInfraEnv, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
			utils_test.IgnoreStateInfo)

		By("update the host's role to worker and check validation")
		hostReq := &installer.V2UpdateHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h1.ID,
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
		}
		h1 = updateHostV2(ctx, hostReq)
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(*c.Status).To(Equal(models.ClusterStatusInsufficient))
	})

	It("Verify garbage collector inactive cluster and infraenv deregistration", func() {
		By("Update cluster's updated_at attribute to become eligible for deregistration due to inactivity")
		cluster := utils_test.TestContext.GetCluster(clusterID)
		db.Model(&cluster).UpdateColumn("updated_at", time.Now().AddDate(-1, 0, 0))

		By("Fetch cluster to make sure it was permanently removed by the garbage collector")
		Eventually(func() error {
			_, err := common.GetClusterFromDBWhere(db, common.SkipEagerLoading, common.SkipDeletedRecords, "id = ?", clusterID)
			return err
		}, "1m", "10s").Should(HaveOccurred())

		By("Update infraEnv's updated_at attribute to become eligible for deregistration due to inactivity")
		infraEnv, err := common.GetInfraEnvFromDBWhere(db, "id = ?", boundInfraEnv)
		Expect(err).NotTo(HaveOccurred())
		db.Model(&infraEnv).UpdateColumn("updated_at", time.Now().AddDate(-1, 0, 0))

		By("Fetch bounded InfraEnv to make sure it was permanently removed by the garbage collector")
		Eventually(func() error {
			_, err = common.GetInfraEnvFromDBWhere(db, "id = ?", boundInfraEnv)
			return err
		}, "1m", "10s").Should(HaveOccurred())

		By("Verify that hosts are deleted")
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h2.ID,
		})
		Expect(err).To(HaveOccurred())
		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
			InfraEnvID: boundInfraEnv,
			HostID:     *h3.ID,
		})
		Expect(err).To(HaveOccurred())

		By("Verify that late-binding infraenv is not deleted")
		_, err = common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnvID)
		Expect(err).NotTo(HaveOccurred())

		By("Update late-binding infraEnv's updated_at attribute to become eligible for deregistration due to inactivity")
		infraEnv, err = common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnvID)
		Expect(err).NotTo(HaveOccurred())
		db.Model(&infraEnv).UpdateColumn("updated_at", time.Now().AddDate(-1, 0, 0))

		By("Fetch late-binding InfraEnv to make sure it was permanently removed by the garbage collector")
		Eventually(func() error {
			_, err := common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnvID)
			return err
		}, "1m", "10s").Should(HaveOccurred())
	})

	It("Verify install config overrides", func() {
		By("returning an error when provided an invalid override")
		override := `{"foo": "bar"}`
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}

		_, err := utils_test.TestContext.UserBMClient.Installer.V2UpdateClusterInstallConfig(ctx, &params)
		Expect(err).NotTo(BeNil())

		By("verifying that the cluster install config override was not set")
		c := utils_test.TestContext.GetCluster(clusterID)
		Expect(c.InstallConfigOverrides).To(BeEmpty())

		By("verifying the feature usage for install config overrides was not set")
		utils_test.VerifyUsageNotSet(c.FeatureUsage, "Install Config Overrides")

		By("succeeding when provided a valid override")
		override = `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params.InstallConfigParams = override
		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateClusterInstallConfig(ctx, &params)
		Expect(err).To(BeNil())

		By("verify that the cluster install config override is correctly updated")
		c = utils_test.TestContext.GetCluster(clusterID)
		Expect(c.InstallConfigOverrides).To(Equal(params.InstallConfigParams))

		By("verifying the feature usage for install config overrides was set")
		overrideUsageProps := make(map[string]interface{})
		overrideUsageProps["controlPlane hyperthreading"] = true
		overrideUsage := models.Usage{
			Name: usage.InstallConfigOverrides,
			Data: overrideUsageProps,
		}
		utils_test.VerifyUsageSet(c.FeatureUsage, overrideUsage)

		By("failing when provided an invalid override")
		originalOverride := override
		override = `{"foo": "bar", "controlPlane": {"hyperthreading": "Enabled"}}`
		params.InstallConfigParams = override
		_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateClusterInstallConfig(ctx, &params)
		Expect(err).ToNot(BeNil())

		By("verify that the cluster install config override did not get updated")
		c = utils_test.TestContext.GetCluster(clusterID)
		Expect(c.InstallConfigOverrides).To(Equal(originalOverride))

		By("verifying the feature usage for install config overrides was not changed")
		utils_test.VerifyUsageSet(c.FeatureUsage, overrideUsage)
	})
})

var _ = Describe("[V2ClusterTests] multiarch", func() {
	Context("ARM", func() {
		ctx := context.Background()

		var tmpBMClient *client.AssistedInstall
		var tmpAgentBMClient *client.AssistedInstall
		var tmpPullSecret string
		var clusterID strfmt.UUID
		var X86infraEnvID strfmt.UUID
		var ARMinfraEnvID strfmt.UUID
		var ips []string
		var h1, h2, h3 *models.Host

		BeforeEach(func() {
			// (MGMT-11859) "user2" has permissions to use multiarch, "user" does not
			tmpBMClient = utils_test.TestContext.UserBMClient
			utils_test.TestContext.UserBMClient = utils_test.TestContext.User2BMClient
			tmpAgentBMClient = utils_test.TestContext.AgentBMClient
			utils_test.TestContext.AgentBMClient = utils_test.TestContext.Agent2BMClient
			tmpPullSecret = pullSecret
			pullSecret = fmt.Sprintf(psTemplate, utils_test.FakePS2)

			clusterReq, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(multiarchOpenshiftVersion),
					PullSecret:       swag.String(pullSecret),
					BaseDNSDomain:    "example.com",
					// If for the same version there is both single-arch and multi-arch release image, the logic
					// implemented in https://github.com/openshift/assisted-service/pull/4314 will not kick in.
					// For this reason in subsystem tests for 4.11 we are explicitly setting CPU architecture
					// so that we use multi-arch. Once we don't carry single-arch release images anymore, this
					// will not be needed.
					CPUArchitecture: common.MultiCPUArchitecture,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			clusterID = *clusterReq.GetPayload().ID
			clusterArch := clusterReq.GetPayload().CPUArchitecture
			Expect(clusterArch).To(Equal(common.MultiCPUArchitecture))

			// standalone x86 infraEnv
			infraEnv := registerInfraEnv(nil, models.ImageTypeFullIso)
			X86infraEnvID = *infraEnv.ID

			// bound arm64 infraEnv
			infraEnv = registerInfraEnvSpecificVersionAndArch(&clusterID, models.ImageTypeFullIso, common.ARM64CPUArchitecture, "")
			ARMinfraEnvID = *infraEnv.ID

			By("register h2 h3 to cluster via the bound arm64 infraenv")
			ips = hostutil.GenerateIPv4Addresses(3, utils_test.DefaultCIDRv4)
			h2 = utils_test.TestContext.RegisterNode(ctx, ARMinfraEnvID, "h2", ips[1])
			h3 = utils_test.TestContext.RegisterNode(ctx, ARMinfraEnvID, "h3", ips[2])
			utils_test.TestContext.V2UpdateVipParams(ctx, clusterID)
			waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, utils_test.DefaultWaitForClusterStateTimeout,
				utils_test.IgnoreStateInfo)
		})

		AfterEach(func() {
			// (MGMT-11859) Reverting the switch from "user" to "user2" that is needed only to test
			//              access to multiarch release images in environments with org-based control
			utils_test.TestContext.UserBMClient = tmpBMClient
			pullSecret = tmpPullSecret
			utils_test.TestContext.AgentBMClient = tmpAgentBMClient
		})

		It("Bind single host to x86 unbound infraenv", func() {
			By("register h1 with the unbound infraenv")
			h1 = &utils_test.TestContext.RegisterHost(X86infraEnvID).Host
			host := utils_test.TestContext.GetHostV2(X86infraEnvID, *h1.ID)
			Expect(host.ClusterID).To(BeNil())

			utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
			waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, h1)

			By("bind h1 to cluster")
			utils_test.TestContext.BindHost(X86infraEnvID, *h1.ID, clusterID)
			waitForHostStateV2(ctx, models.HostStatusBinding, utils_test.DefaultWaitForHostStateTimeout, h1)

			By("register h1 again and define the connectivity to the other hosts")
			h1 = &utils_test.TestContext.RegisterHostByUUID(h1.InfraEnvID, *h1.ID).Host

			utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
			generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
			waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

			By("cluster is ready")
			utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
			waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
				utils_test.IgnoreStateInfo)
		})

		It("Bind single host to arm64 bound infraenv", func() {
			By("register h1 with the bound infraenv")
			h1 = &utils_test.TestContext.RegisterHost(ARMinfraEnvID).Host
			host := utils_test.TestContext.GetHostV2(ARMinfraEnvID, *h1.ID)
			Expect(host.ClusterID).NotTo(BeNil())

			utils_test.TestContext.GenerateHWPostStepReply(ctx, h1, utils_test.GetDefaultInventory(ips[0]), "h1")
			utils_test.TestContext.GenerateEssentialHostSteps(ctx, h1, "h1", ips[0])
			generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
			waitForHostStateV2(ctx, models.HostStatusKnown, utils_test.DefaultWaitForHostStateTimeout, h1)

			By("cluster is ready")
			utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, h1)
			waitForClusterState(ctx, clusterID, models.ClusterStatusReady, utils_test.DefaultWaitForClusterStateTimeout,
				utils_test.IgnoreStateInfo)
		})

		It("Fail to register an infraenv with a non-supported CPUArchitecture ", func() {
			_, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env"),
					OpenshiftVersion: multiarchOpenshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeFullIso,
					ClusterID:        &clusterID,
					CPUArchitecture:  "fake-chocobomb-architecture",
				},
			})

			Expect(err).To(HaveOccurred())
		})

	})

	Context("s390x", func() {
		ctx := context.Background()

		var tmpBMClient *client.AssistedInstall
		var tmpAgentBMClient *client.AssistedInstall
		var tmpPullSecret string
		var clusterID strfmt.UUID

		registerClusterForMultiArch := func() *models.Cluster {
			clusterReq, err := utils_test.TestContext.User2BMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                  swag.String("test-cluster"),
					OpenshiftVersion:      swag.String(multiarchOpenshiftVersion),
					PullSecret:            swag.String(fmt.Sprintf(psTemplate, utils_test.FakePS2)),
					BaseDNSDomain:         "example.com",
					UserManagedNetworking: swag.Bool(true),
					CPUArchitecture:       common.S390xCPUArchitecture,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterReq.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))

			return clusterReq.Payload
		}
		BeforeEach(func() {
			// (MGMT-11859) "user2" has permissions to use multiarch, "user" does not
			tmpBMClient = utils_test.TestContext.UserBMClient
			utils_test.TestContext.UserBMClient = utils_test.TestContext.User2BMClient
			tmpAgentBMClient = utils_test.TestContext.AgentBMClient
			utils_test.TestContext.AgentBMClient = utils_test.TestContext.Agent2BMClient
			tmpPullSecret = pullSecret
			pullSecret = fmt.Sprintf(psTemplate, utils_test.FakePS2)

		})

		AfterEach(func() {
			// (MGMT-11859) Reverting the switch from "user" to "user2" that is needed only to test
			//              access to multiarch release images in environments with org-based control
			utils_test.TestContext.UserBMClient = tmpBMClient
			pullSecret = tmpPullSecret
			utils_test.TestContext.AgentBMClient = tmpAgentBMClient
		})

		It("Default image type on s390x", func() {
			cluster := registerClusterForMultiArch()
			infraEnv := registerInfraEnvSpecificVersionAndArch(cluster.ID, "", common.S390xCPUArchitecture, "")
			Expect(*infraEnv.Type).To(Equal(models.ImageTypeFullIso))

			_, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(pullSecret),
					SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
					ImageType:        models.ImageTypeMinimalIso,
					ClusterID:        &clusterID,
					CPUArchitecture:  common.S390xCPUArchitecture,
				},
			})

			Expect(err).To(HaveOccurred())
		})
		It("Default image type on ppc64le", func() {
			cluster := registerClusterForMultiArch()
			infraEnv := registerInfraEnvSpecificVersionAndArch(cluster.ID, "", common.PowerCPUArchitecture, "")
			Expect(*infraEnv.Type).To(Equal(models.ImageTypeFullIso))

			infraEnv = registerInfraEnvSpecificVersionAndArch(cluster.ID, models.ImageTypeMinimalIso, common.PowerCPUArchitecture, "")
			Expect(*infraEnv.Type).To(Equal(models.ImageTypeMinimalIso))
		})

	})

})
