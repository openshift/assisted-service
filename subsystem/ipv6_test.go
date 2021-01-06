package subsystem

import (
	"context"
	"fmt"
	"time"

	"github.com/alecthomas/units"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var (
	validHwInfoV6 = &models.Inventory{
		CPU:    &models.CPU{Count: 16},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
		Disks: []*models.Disk{
			{DriveType: "SSD", Name: "loop0", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
		Interfaces: []*models.Interface{
			{
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Timestamp:    1601853088,
	}
)

var _ = Describe("IPv6 installation", func() {
	var (
		ctx         = context.Background()
		cluster     *models.Cluster
		clusterCIDR = "2002:db8::/53"
		serviceCIDR = "2003:db8::/112"
		clusterID   strfmt.UUID
	)

	AfterEach(func() {
		clearDB()
	})
	BeforeEach(func() {
		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 64,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:               swag.String(pullSecret),
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		clusterID = *cluster.ID
		log.Infof("Register cluster %s", cluster.ID.String())
	})
	It("install_cluster IPv6 happy flow", func() {
		_ = registerHostsAndSetRolesV6(clusterID, 5)
		By("Installing cluster till finalize")
		c := installCluster(clusterID)
		Expect(swag.StringValue(c.Status)).Should(Equal("installing"))
		Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
		Expect(len(c.Hosts)).Should(Equal(5))
		for _, host := range c.Hosts {
			Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
		}

		for _, host := range c.Hosts {
			updateProgress(*host.ID, clusterID, models.HostStageDone)
		}

		waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
		By("Completing installation installation")
		success := true
		_, err := agentBMClient.Installer.CompleteInstallation(ctx,
			&installer.CompleteInstallationParams{ClusterID: clusterID, CompletionParams: &models.CompletionParams{IsSuccess: &success, ErrorInfo: ""}})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying installation successfully completed")
		waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, "installed")
	})
})

func registerHostsAndSetRolesV6(clusterID strfmt.UUID, numHosts int) []*models.Host {
	ctx := context.Background()
	hosts := make([]*models.Host, 0)

	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := &registerHost(clusterID).Host
		generateHWPostStepReply(ctx, host, validHwInfoV6, hostname)
		generateNTPPostStepReply(ctx, host, validNtpSources)
		var role models.HostRoleUpdateParams
		if i < 3 {
			role = models.HostRoleUpdateParamsMaster
		} else {
			role = models.HostRoleUpdateParamsWorker
		}
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *host.ID, Role: role},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		hosts = append(hosts, host)
	}
	generateFullMeshConnectivity(ctx, "1001:db8::10", hosts...)
	apiVip := "1001:db8::64"
	ingressVip := "1001:db8::65"
	_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVip:            &apiVip,
			IngressVip:        &ingressVip,
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

	return hosts
}
