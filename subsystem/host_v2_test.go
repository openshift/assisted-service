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
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("Host tests v2", func() {
	ctx := context.Background()
	var infraEnv *installer.RegisterInfraEnvCreated
	var infraEnvID strfmt.UUID
	var cluster *installer.V2RegisterClusterCreated
	var clusterID strfmt.UUID

	BeforeEach(func() {
		var err error
		infraEnv, err = utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
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

		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
	})

	It("host infra env CRUD", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering-unbound"))
		Expect(host.StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		list, err := utils_test.TestContext.UserBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		list, err = utils_test.TestContext.UserBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
			InfraEnvID: infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).Should(HaveOccurred())
	})

	It("infra-env host should reach know-unbound state", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		waitForHostStateV2(ctx, models.HostStatusDiscoveringUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
		host = updateInventory(ctx, infraEnvID, *host.ID, defaultInventory())
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
	})

	It("update_hostname_successfully", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		host = updateInventory(ctx, infraEnvID, *host.ID, defaultInventory())

		hostnameRequest := &installer.V2UpdateHostParams{
			InfraEnvID: infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				HostName: swag.String("new-host-name"),
			},
		}
		updatedHost := updateHostV2(ctx, hostnameRequest)
		Expect(updatedHost.RequestedHostname).To(Equal("new-host-name"))
	})

	It("update node labels successfully", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		host = updateInventory(ctx, infraEnvID, *host.ID, defaultInventory())

		nodeLabelsList := []*models.NodeLabelParams{
			{
				Key:   swag.String("node.ocs.openshift.io/storage"),
				Value: swag.String(""),
			},
		}

		req := &installer.V2UpdateHostParams{
			InfraEnvID: infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				NodeLabels: nodeLabelsList,
			},
		}
		updatedHost := updateHostV2(ctx, req)
		nodeLabelsStr, _ := common.MarshalNodeLabels(nodeLabelsList)
		Expect(updatedHost.NodeLabels).To(Equal(nodeLabelsStr))
	})

	It("update infra-env host installation disk id success", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		inventory, error := common.UnmarshalInventory(defaultInventory())
		Expect(error).ToNot(HaveOccurred())
		inventory.Disks = []*models.Disk{
			{
				ID:        "wwn-0x1111111111111111111111",
				ByID:      "wwn-0x1111111111111111111111",
				DriveType: "HDD",
				Name:      "sda",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
			{
				ID:        "wwn-0x2222222222222222222222",
				ByID:      "wwn-0x2222222222222222222222",
				DriveType: "HDD",
				Name:      "sdb",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
		}

		inventoryStr, err := common.MarshalInventory(inventory)
		Expect(err).ToNot(HaveOccurred())
		host = updateInventory(ctx, infraEnvID, *host.ID, inventoryStr)

		Expect(host.InstallationDiskID).To(Equal(inventory.Disks[0].ID))
		Expect(host.InstallationDiskPath).To(Equal(fmt.Sprintf("/dev/%s", inventory.Disks[0].Name)))

		diskSelectionRequest := &installer.V2UpdateHostParams{
			InfraEnvID: infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				DisksSelectedConfig: []*models.DiskConfigParams{
					{ID: &inventory.Disks[1].ID, Role: models.DiskRoleInstall},
					{ID: &inventory.Disks[0].ID, Role: models.DiskRoleNone},
				},
			},
		}

		_, error = utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, diskSelectionRequest)
		Expect(error).ToNot(HaveOccurred())
	})

	It("register_same_host_id", func() {
		// register to infra-env 1
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		hostID := *host.ID

		infraEnv2, err := utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
			InfraenvCreateParams: &models.InfraEnvCreateParams{
				Name:             swag.String("another test-infra-env"),
				OpenshiftVersion: openshiftVersion,
				PullSecret:       swag.String(pullSecret),
				SSHAuthorizedKey: swag.String(utils_test.SshPublicKey),
				ImageType:        models.ImageTypeFullIso,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		infraEnvID2 := *infraEnv2.GetPayload().ID

		// register to infra env2
		_ = utils_test.TestContext.RegisterHostByUUID(infraEnvID2, hostID)

		// successfully get from both clusters
		_ = utils_test.TestContext.GetHostV2(infraEnvID, hostID)
		_ = utils_test.TestContext.GetHostV2(infraEnvID2, hostID)

		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		})
		Expect(err).NotTo(HaveOccurred())
		h := utils_test.TestContext.GetHostV2(infraEnvID2, hostID)

		// register again to cluster 2 and expect it to be in discovery status
		Expect(db.Model(h).Update("status", "known-unbound").Error).NotTo(HaveOccurred())
		h = utils_test.TestContext.GetHostV2(infraEnvID2, hostID)
		Expect(swag.StringValue(h.Status)).Should(Equal("known-unbound"))
		_ = utils_test.TestContext.RegisterHostByUUID(infraEnvID2, hostID)
		h = utils_test.TestContext.GetHostV2(infraEnvID2, hostID)
		Expect(swag.StringValue(h.Status)).Should(Equal("discovering-unbound"))
	})

	It("bind host", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		waitForHostStateV2(ctx, models.HostStatusDiscoveringUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
		host = updateInventory(ctx, infraEnvID, *host.ID, defaultInventory())
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
		host = utils_test.TestContext.BindHost(host.InfraEnvID, *host.ID, clusterID)
		Expect(host.ClusterID).NotTo(BeNil())
		Expect(*host.ClusterID).Should(Equal(clusterID))
		waitForHostStateV2(ctx, models.HostStatusBinding, utils_test.DefaultWaitForHostStateTimeout, host)
		steps := utils_test.TestContext.GetNextSteps(host.InfraEnvID, *host.ID)
		Expect(len(steps.Instructions)).Should(Equal(0))
	})

	It("bind host in insufficient state should fail", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		waitForHostStateV2(ctx, models.HostStatusDiscoveringUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
		By("move the host to insufficient")
		Expect(db.Model(host).UpdateColumns(&models.Host{Inventory: defaultInventory(),
			Status:             swag.String(models.HostStatusInsufficient),
			InstallationDiskID: "wwn-0x1111111111111111111111"}).Error).NotTo(HaveOccurred())
		By("reject host in insufficient state")
		_, err := utils_test.TestContext.UserBMClient.Installer.BindHost(context.Background(), &installer.BindHostParams{
			HostID:     *host.ID,
			InfraEnvID: infraEnvID,
			BindHostParams: &models.BindHostParams{
				ClusterID: &clusterID,
			},
		})
		Expect(err).NotTo(BeNil())
	})
})

var _ = Describe("Day2 Host tests v2", func() {
	ctx := context.Background()
	var infraEnv *installer.RegisterInfraEnvCreated
	var infraEnvID strfmt.UUID
	var cluster *installer.V2RegisterClusterCreated
	var clusterID strfmt.UUID

	BeforeEach(func() {
		var err error
		infraEnv, err = utils_test.TestContext.UserBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
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

		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
		Expect(db.Model(cluster.GetPayload()).Update("kind", swag.String(models.ClusterKindAddHostsCluster)).Error).NotTo(HaveOccurred())
	})

	It("bind host to day2 cluster", func() {
		host := &utils_test.TestContext.RegisterHost(infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		Expect(*host.Status).Should(Equal("discovering-unbound"))
		Expect(host.StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		waitForHostStateV2(ctx, models.HostStatusDiscoveringUnbound, utils_test.DefaultWaitForHostStateTimeout, host)
		host = updateInventory(ctx, infraEnvID, *host.ID, defaultInventory())
		waitForHostStateV2(ctx, models.HostStatusKnownUnbound, utils_test.DefaultWaitForHostStateTimeout, host)

		host = utils_test.TestContext.BindHost(infraEnvID, *host.ID, clusterID)
		Expect(swag.StringValue(host.Status)).Should(Equal("binding"))

		host = &utils_test.TestContext.RegisterHostByUUID(infraEnvID, *host.ID).Host
		host = utils_test.TestContext.GetHostV2(host.InfraEnvID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("discovering"))
		Expect(swag.StringValue(host.Kind)).Should(Equal(models.HostKindAddToExistingClusterHost))
	})
})

func updateHostV2(ctx context.Context, request *installer.V2UpdateHostParams) *models.Host {
	response, error := utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, request)
	Expect(error).ShouldNot(HaveOccurred())
	Expect(response).NotTo(BeNil())
	Expect(response.Payload).NotTo(BeNil())
	return response.Payload
}

func isHostInStateV2(ctx context.Context, host *models.Host, state string) (bool, string, string) {
	rep, err := utils_test.TestContext.UserBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{InfraEnvID: host.InfraEnvID, HostID: *host.ID})
	Expect(err).NotTo(HaveOccurred())
	h := rep.GetPayload()
	return swag.StringValue(h.Status) == state, swag.StringValue(h.Status), swag.StringValue(h.StatusInfo)
}

func waitForHostStateV2(ctx context.Context, state string, timeout time.Duration, host *models.Host) {
	Eventually(func() error {
		success, lastState, lastStatusInfo := isHostInStateV2(ctx, host, state)
		if success {
			return nil
		}

		return fmt.Errorf("Host %s in Infra Env %s wasn't in state %s after %d seconds in a row. Actual state %s, state info %s",
			*host.ID, host.InfraEnvID, state, timeout, lastState, lastStatusInfo)

	}, timeout, time.Second).Should(BeNil())
	Consistently(func() error {
		success, lastState, lastStatusInfo := isHostInStateV2(ctx, host, state)
		if success {
			return nil
		}
		return fmt.Errorf("Host %s in Infra Env %s switched backed to state %s, state info %s.",
			*host.ID, host.InfraEnvID, lastState, lastStatusInfo)
	}, 10, 1).Should(Succeed())
}

func defaultInventory() string {
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

		// CPU, Disks, and Memory were added here to prevent the case that assisted-service crashes in case the monitor starts
		// working in the middle of the test and this inventory is in the database.
		CPU: &models.CPU{
			Count: 4,
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
		Hostname: uuid.New().String(),
		Memory: &models.Memory{
			PhysicalBytes: int64(16) * (int64(1) << 30),
			UsableBytes:   int64(16) * (int64(1) << 30),
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}
