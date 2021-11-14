package subsystem

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("[V2ClusterTests]", func() {
	ctx := context.Background()
	var clusterID strfmt.UUID
	var infraEnvID strfmt.UUID
	var boundInfraEnv strfmt.UUID
	var ips []string
	var h1, h2, h3 *models.Host

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		clusterReq, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				BaseDNSDomain:            "example.com",
				ClusterNetworkHostPrefix: 23,
			},
		})

		Expect(err).NotTo(HaveOccurred())
		clusterID = *clusterReq.GetPayload().ID

		//standalone infraEnv
		infraEnv := registerInfraEnv(nil)
		infraEnvID = *infraEnv.ID

		//bound infraEnv
		infraEnv = registerInfraEnv(&clusterID)
		boundInfraEnv = *infraEnv.ID

		By("register h2 h3 to cluster via the bound infraEnv")
		ips = hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		h2 = registerNode(ctx, boundInfraEnv, "h2", ips[1])
		h3 = registerNode(ctx, boundInfraEnv, "h3", ips[2])
		v2UpdateVipParams(ctx, clusterID)
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
	})

	It("Bind/Unbind single host in 3 nodes cluster (standalone InfraEnv)", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with the stand-alone InfraEnv")
		h1 = &registerHost(infraEnvID).Host
		generateHWPostStepReply(ctx, h1, getDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, defaultWaitForHostStateTimeout, h1)

		//bind the 3rd node and re-register it
		By("bind h1 to cluster")
		bindHost(infraEnvID, *h1.ID, clusterID)
		waitForHostStateV2(ctx, models.HostStatusBinding, defaultWaitForHostStateTimeout, h1)

		By("register h1 again and define the connectivity to the other hosts")
		h1 = &registerHostByUUID(h1.InfraEnvID, *h1.ID).Host

		generateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		generateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("verify host name is set")
		h1 = getHostV2(infraEnvID, *h1.ID)
		Expect(h1.RequestedHostname).To(Equal("h1"))

		By("unbind host and re-register h1 --> cluster return to insufficient")
		unbindHost(infraEnvID, *h1.ID)
		h1 = &registerHost(infraEnvID).Host
		generateHWPostStepReply(ctx, h1, getDefaultInventory(ips[0]), "h1")
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, defaultWaitForHostStateTimeout, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("verify that the unbound host still retains its name and disks count")
		h1 = getHostV2(infraEnvID, *h1.ID)
		Expect(h1.RequestedHostname).To(Equal(("h1")))
		var inventory models.Inventory
		_ = json.Unmarshal([]byte(h1.Inventory), &inventory)
		Expect(len(inventory.Disks)).To(Equal(2))
	})

	It("register single host in 3 nodes cluster (bound InfraEnv)", func() {
		//register node to the InfraEnv and get its inventory
		By("register h1 with the bound InfraEnv (implicit binding)")
		h1 = &registerHost(boundInfraEnv).Host
		host := getHostV2(boundInfraEnv, *h1.ID)
		Expect(host.ClusterID).NotTo(BeNil())

		generateHWPostStepReply(ctx, h1, getDefaultInventory(ips[0]), "h1")
		generateEssentialHostSteps(ctx, h1, "h1", ips[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostStateV2(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h1)

		By("cluster is ready")
		generateEssentialPrepareForInstallationSteps(ctx, h1)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("unbind host should fail since infraEnv is bound to cluster")
		_, err := userBMClient.Installer.UnbindHost(ctx, &installer.UnbindHostParams{
			HostID:     *h1.ID,
			InfraEnvID: boundInfraEnv,
		})
		Expect(err).NotTo(BeNil())
	})
})
