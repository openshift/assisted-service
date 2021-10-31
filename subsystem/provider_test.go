package subsystem

import (
	"context"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Provider subsystem tests", func() {
	ctx := context.Background()
	var clusterID strfmt.UUID

	BeforeEach(func() {
		cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				BaseDNSDomain:    "example.com",
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
		// in order to simulate infra env generation
		generateClusterISO(clusterID, models.ImageTypeMinimalIso)
	})

	AfterEach(func() {
		clearDB()
	})

	It("install cluster with oVirt hosts", func() {
		By("set hosts with hw info and setup essential host steps")
		inventoryInfo := oVirtInventory()
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		h1 := registerNode(ctx, clusterID, "h1", ips[0])
		h2 := registerNode(ctx, clusterID, "h2", ips[1])
		h3 := registerNode(ctx, clusterID, "h3", ips[2])
		generateEssentialHostStepsWithInventory(ctx, h1, "h1", inventoryInfo)
		generateEssentialHostStepsWithInventory(ctx, h2, "h2", inventoryInfo)
		generateEssentialHostStepsWithInventory(ctx, h3, "h3", inventoryInfo)
		apiVip := "1.2.3.8"
		ingressVip := "1.2.3.9"
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVip:            &apiVip,
				IngressVip:        &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Set hosts conectivity")
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h1)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h2)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h3)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h2.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h3.ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("start installation")
		installCluster(clusterID)
		waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
	})

	It("host installation progress with ovirt invntory", func() {
		host := &registerHost(clusterID).Host
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", oVirtInventory()).Error).NotTo(HaveOccurred())

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

	It("install cluster with 2 ovirt hosts and one generic", func() {
		By("set hosts with hw info and setup essential host steps")
		inventoryOvirtInfo := oVirtInventory()
		inventoryBMInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
			Disks:  []*models.Disk{&loop0, &sdb},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						defaultCIDRv4,
					},
					MacAddress: "e6:53:3d:a7:77:b4",
				},
			},
			SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "RHEL", SerialNumber: "3534"},
			Timestamp:    1601853088,
			Routes:       common.TestDefaultRouteConfiguration,
			TpmVersion:   models.InventoryTpmVersionNr20,
		}
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		h1 := registerNode(ctx, clusterID, "h1", ips[0])
		h2 := registerNode(ctx, clusterID, "h2", ips[1])
		h3 := registerNode(ctx, clusterID, "h3", ips[2])
		generateEssentialHostStepsWithInventory(ctx, h1, "h1", inventoryOvirtInfo)
		generateEssentialHostStepsWithInventory(ctx, h2, "h2", inventoryOvirtInfo)
		generateEssentialHostStepsWithInventory(ctx, h3, "h3", inventoryBMInfo)
		apiVip := "1.2.3.8"
		ingressVip := "1.2.3.9"
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVip:            &apiVip,
				IngressVip:        &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Set hosts conectivity")
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h1)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h2)
		waitForHostState(ctx, clusterID, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h3)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h2.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h3.ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("start installation")
		installCluster(clusterID)
		waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
	})
})

func oVirtInventory() *models.Inventory {
	inv := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				SpeedMbps:  20,
				MacAddress: "e6:53:3d:a7:77:b4",
			},
			{
				Name: "eth1",
				IPV4Addresses: []string{
					"1.2.5.4/24",
				},
				SpeedMbps: 40,
			},
		},
		CPU: &models.CPU{
			Count: 8,
		},
		Disks: []*models.Disk{
			{
				ID:        "wwn-0x1111111111111111111111",
				ByID:      "wwn-0x1111111111111111111111",
				DriveType: "HDD",
				Name:      "sda1",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
		},
		Memory: &models.Memory{
			PhysicalBytes: int64(32 * units.GiB),
			UsableBytes:   int64(32 * units.GiB),
		},
		SystemVendor: &models.SystemVendor{
			Manufacturer: "oVirt",
			ProductName:  "oVirt",
			SerialNumber: "3534",
			Virtual:      true,
		},
		Timestamp:  1601845851,
		Routes:     common.TestDefaultRouteConfiguration,
		TpmVersion: models.InventoryTpmVersionNr20,
	}
	return &inv
}
