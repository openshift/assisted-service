package subsystem

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/conversions"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	"k8s.io/utils/ptr"
)

const (
	clusterInsufficientStateInfo                = "Cluster is not ready for install"
	clusterReadyStateInfo                       = "Cluster ready to be installed"
	IgnoreStateInfo                             = "IgnoreStateInfo"
	clusterCanceledInfo                         = "Canceled cluster installation"
	clusterErrorInfo                            = "cluster has hosts in error"
	clusterResetStateInfo                       = "cluster was reset by user"
	clusterPendingForInputStateInfo             = "User input required"
	clusterFinalizingStateInfo                  = "Finalizing cluster installation"
	clusterInstallingPendingUserActionStateInfo = "Cluster has hosts with wrong boot order"
	clusterInstallingStateInfo                  = "Installation in progress"

	ingressCa = "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
		"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
		"MQswCQYDVQQHDAJkZDELMAkGA1UECgwCZGQxCzAJBgNVBAsMAmRkMQswCQYDVQQDDAJkZDERMA8GCSqGSIb3DQEJARYCZGQwHhcNMjAwNTI1MTYwNTAwWhcNMzA" +
		"wNTIzMTYwNTAwWjBhMQswCQYDVQQGEwJpczELMAkGA1UECAwCZGQxCzAJBgNVBAcMAmRkMQswCQYDVQQKDAJkZDELMAkGA1UECwwCZGQxCzAJBgNVBAMMAmRkMREwDwYJKoZIh" +
		"vcNAQkBFgJkZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAML63CXkBb+lvrJKfdfYBHLDYfuaC6exCSqASUAosJWWrfyDiDMUbmfs06PLKyv7N8efDhza74ov0EQJ" +
		"NRhMNaCE+A0ceq6ZXmmMswUYFdLAy8K2VMz5mroBFX8sj5PWVr6rDJ2ckBaFKWBB8NFmiK7MTWSIF9n8M107/9a0QURCvThUYu+sguzbsLODFtXUxG5rtTVKBVcPZvEfRky2Tkt4AySFS" +
		"mkO6Kf4sBd7MC4mKWZm7K8k7HrZYz2usSpbrEtYGtr6MmN9hci+/ITDPE291DFkzIcDCF493v/3T+7XsnmQajh6kuI+bjIaACfo8N+twEoJf/N1PmphAQdEiC0CAwEAAaNTMFEwHQYDVR0O" +
		"BBYEFNvmSprQQ2HUUtPxs6UOuxq9lKKpMB8GA1UdIwQYMBaAFNvmSprQQ2HUUtPxs6UOuxq9lKKpMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJEWxnxtQV5IqPVRr2SM" +
		"WNNxcJ7A/wyet39l5VhHjbrQGynk5WS80psn/riLUfIvtzYMWC0IR0pIMQuMDF5sNcKp4D8Xnrd+Bl/4/Iy/iTOoHlw+sPkKv+NL2XR3iO8bSDwjtjvd6L5NkUuzsRoSkQCG2fHASqqgFoyV9Ld" +
		"RsQa1w9ZGebtEWLuGsrJtR7gaFECqJnDbb0aPUMixmpMHID8kt154TrLhVFmMEqGGC1GvZVlQ9Of3GP9y7X4vDpHshdlWotOnYKHaeu2d5cRVFHhEbrslkISgh/TRuyl7VIpnjOYUwMBpCiVH6M" +
		"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
)

const (
	validDiskSize     = int64(128849018880)
	minSuccessesInRow = 2
	minHosts          = 3
	loop0Id           = "wwn-0x1111111111111111111111"
	sdbId             = "wwn-0x2222222222222222222222"
	defaultCIDRv4     = "1.2.3.10/24"
)

var (
	loop0 = models.Disk{
		ID:        loop0Id,
		ByID:      loop0Id,
		DriveType: "SSD",
		Name:      "loop0",
		SizeBytes: validDiskSize,
	}

	sdb = models.Disk{
		ID:        sdbId,
		ByID:      sdbId,
		DriveType: "HDD",
		Name:      "sdb",
		SizeBytes: validDiskSize,
	}

	vma = models.Disk{
		ID:        sdbId,
		ByID:      sdbId,
		DriveType: "HDD",
		Name:      "vma",
		HasUUID:   true,
		Vendor:    "VMware",
		SizeBytes: validDiskSize,
	}

	vmremovable = models.Disk{
		ID:        loop0Id,
		ByID:      loop0Id,
		DriveType: "0DD",
		Name:      "sr0",
		Removable: true,
		SizeBytes: 106516480,
	}

	validHwInfo = &models.Inventory{
		CPU:    &models.CPU{Count: 16, Architecture: "x86_64"},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
		Disks:  []*models.Disk{&loop0, &sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					defaultCIDRv4,
				},
				MacAddress: "e6:53:3d:a7:77:b4",
				Type:       "physical",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
		TpmVersion:   models.InventoryTpmVersionNr20,
	}

	validFreeAddresses = models.FreeNetworksAddresses{
		{
			Network: "1.2.3.0/24",
			FreeAddresses: []strfmt.IPv4{
				"1.2.3.8",
				"1.2.3.9",
				"1.2.3.5",
				"1.2.3.6",
				"1.2.3.100",
				"1.2.3.101",
				"1.2.3.102",
				"1.2.3.103",
			},
		},
	}
)

func getMinimalMasterInventory(cidr string) *models.Inventory {
	inventory := *getDefaultInventory(cidr)
	inventory.CPU = &models.CPU{Count: 4}
	inventory.Memory = &models.Memory{PhysicalBytes: int64(16 * units.GiB), UsableBytes: int64(16 * units.GiB)}
	return &inventory
}

func getValidWorkerHwInfoWithCIDR(cidr string) *models.Inventory {
	return &models.Inventory{
		CPU:    &models.CPU{Count: 2},
		Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB), UsableBytes: int64(8 * units.GiB)},
		Disks:  []*models.Disk{&loop0, &sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					cidr,
				},
				MacAddress: "e6:53:3d:a7:77:b4",
				Type:       "physical",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
}

var _ = Describe("Cluster with Platform", func() {
	ctx := context.Background()

	Context("vSphere", func() {
		It("vSphere cluster on OCP 4.12 - Success", func() {
			cluster, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("test-cluster"),
					OpenshiftVersion:     swag.String("4.12"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					PullSecret:           swag.String(pullSecret),
					Platform:             &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(*cluster.GetPayload().Platform.Type).Should(Equal(models.PlatformTypeVsphere))
		})

		It("vSphere cluster on OCP 4.12 with dual stack - Failure", func() {
			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("test-cluster"),
					OpenshiftVersion:     swag.String("4.12"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					PullSecret:           swag.String(pullSecret),
					Platform:             &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
					MachineNetworks:      common.TestDualStackNetworking.MachineNetworks,
					ClusterNetworks:      common.TestDualStackNetworking.ClusterNetworks,
					ServiceNetworks:      common.TestDualStackNetworking.ServiceNetworks,
				},
			})
			Expect(err).Should(HaveOccurred())

			// Message can be one of those two:
			// cannot use Dual-Stack because it's not compatible with vSphere Platform Integration
			// cannot use vSphere Platform Integration because it's not compatible with Dual-Stack
			e := err.(*installer.V2RegisterClusterBadRequest)
			Expect(*e.Payload.Reason).To(ContainSubstring("cannot use"))
			Expect(*e.Payload.Reason).To(ContainSubstring("vSphere Platform Integration"))
			Expect(*e.Payload.Reason).To(ContainSubstring("Dual-Stack"))
		})

		It("vSphere cluster on OCP 4.13 with dual stack - Succeess", func() {
			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("test-cluster"),
					OpenshiftVersion:     swag.String("4.13"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					PullSecret:           swag.String(pullSecret),
					Platform:             &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
					MachineNetworks:      common.TestDualStackNetworking.MachineNetworks,
					ClusterNetworks:      common.TestDualStackNetworking.ClusterNetworks,
					ServiceNetworks:      common.TestDualStackNetworking.ServiceNetworks,
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

})

var _ = Describe("Cluster", func() {
	ctx := context.Background()
	var cluster *installer.V2RegisterClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID *strfmt.UUID
	var err error

	BeforeEach(func() {
		cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				BaseDNSDomain:    "example.com",
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
		Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("register an unregistered host success", func() {
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		h := registerHost(*infraEnvID)
		_, err1 := userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *h.ID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		_, err2 := agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: h.ID,
			},
		})
		Expect(err2).ShouldNot(HaveOccurred())
		c := getCluster(clusterID)
		Expect(len(c.Hosts)).Should(Equal(1))
		Expect(c.Hosts[0].ID.String()).Should(Equal(h.ID.String()))
	})

	It("Deregister host triggers cluster validations", func() {
		By("registering 3 nodes in a cluster")
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		c := cluster.GetPayload()
		hosts := registerHostsAndSetRoles(clusterID, *infraEnvID, 3, c.Name, c.BaseDNSDomain)
		By("deregister node and check master count validation")
		_, err = userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *hosts[0].ID,
		})
		Expect(err).ShouldNot(HaveOccurred())
		vStatus, err1 := isClusterValidationInStatus(clusterID, models.ClusterValidationIDSufficientMastersCount, "failure")
		Expect(err1).NotTo(HaveOccurred())
		Expect(vStatus).To(BeTrue())
	})

	It("update cluster name exceed max length (54 characters)", func() {
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				Name: swag.String("loveisintheaireverywhereilookaroundloveisintheaireverysightandeverysound"),
			},
			ClusterID: clusterID,
		})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster name exceed max length (54 characters)", func() {
		_, err1 := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{
			ClusterID: clusterID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).Should(HaveOccurred())
	})

	It("register an unregistered cluster success", func() {
		_, err1 := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{
			ClusterID: clusterID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})

		Expect(err).ShouldNot(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal(models.ClusterStatusInsufficient))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
	})

	It("list clusters - get unregistered cluster", func() {
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		_ = registerHost(*infraEnvID)
		_, err1 := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err1).ShouldNot(HaveOccurred())
		ret, err2 := readOnlyAdminUserBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
		Expect(err2).ShouldNot(HaveOccurred())
		clusters := ret.GetPayload()
		Expect(len(clusters)).ShouldNot(Equal(0))
		var clusterFound models.Cluster
		for _, c := range clusters {
			if c.ID.String() == clusterID.String() {
				clusterFound = *c
				break
			}
		}
		Expect(clusterFound.ID.String()).Should(Equal(clusterID.String()))
		Expect(clusterFound.DeletedAt).ShouldNot(Equal(strfmt.DateTime{}))
		Expect(clusterFound.Hosts).Should(BeEmpty())
	})

	It("list clusters - get unregistered cluster with hosts", func() {
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		_ = registerHost(*infraEnvID)
		_, err1 := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err1).ShouldNot(HaveOccurred())
		ret, err2 := readOnlyAdminUserBMClient.Installer.V2ListClusters(ctx,
			&installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true),
				WithHosts: true,
			})
		Expect(err2).ShouldNot(HaveOccurred())
		clusters := ret.GetPayload()
		Expect(clusters).ShouldNot(BeEmpty())
		var clusterFound models.Cluster
		for _, c := range clusters {
			if c.ID.String() == clusterID.String() {
				clusterFound = *c
				break
			}
		}
		Expect(clusterFound.Hosts).ShouldNot(BeEmpty())
	})

	It("cluster CRUD", func() {
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		_ = registerHost(*infraEnvID)
		Expect(err).NotTo(HaveOccurred())

		getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		getReply, err = agentBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		By("update cluster with valid ssh key")
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		host1 := registerHost(*infraEnvID)
		host2 := registerHost(*infraEnvID)

		validPublicKey := sshPublicKey

		//update host roles with v2 UpdateHost request
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *host1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *host2.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		c, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				SSHPublicKey: &validPublicKey,
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(validPublicKey))

		h := getHostV2(*infraEnvID, *host1.ID)
		Expect(h.Role).Should(Equal(models.HostRole(models.HostRoleUpdateParamsMaster)))

		h = getHostV2(*infraEnvID, *host2.ID)
		Expect(h.Role).Should(Equal(models.HostRole(models.HostRoleUpdateParamsWorker)))

		By("update cluster invalid ssh key")
		invalidPublicKey := `ssh-rsa AAAAB3NzaC1yc2EAAAADAABgQD14Gv4V5DVvyr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUAAbfmn/HalJTHB73LyjilKiPvJ+x2bG7AeiqyVHwtQSpt02FCdQGptmsSqqWF/b9botOO38eUsqPNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= oscohen@localhost.localdomain`

		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				SSHPublicKey: &invalidPublicKey,
			},
			ClusterID: clusterID,
		})
		Expect(err).Should(HaveOccurred())
	})
})

func isClusterInState(ctx context.Context, clusterID strfmt.UUID, state, stateInfo string) (bool, string, string) {
	rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	c := rep.GetPayload()
	if swag.StringValue(c.Status) == state {
		return stateInfo == IgnoreStateInfo ||
			swag.StringValue(c.StatusInfo) == stateInfo, swag.StringValue(c.Status), swag.StringValue(c.StatusInfo)
	}
	ExpectWithOffset(2, swag.StringValue(c.Status)).NotTo(Equal("error"))

	return false, swag.StringValue(c.Status), swag.StringValue(c.StatusInfo)
}

func waitForClusterState(ctx context.Context, clusterID strfmt.UUID, state string, timeout time.Duration, stateInfo string) {
	log.Infof("Waiting for cluster %s status %s", clusterID, state)
	var (
		lastState      string
		lastStatusInfo string
		success        bool
	)

	for start, successInRow := time.Now(), 0; time.Since(start) < timeout; {
		success, lastState, lastStatusInfo = isClusterInState(ctx, clusterID, state, stateInfo)

		if success {
			successInRow++
		} else {
			successInRow = 0
		}

		// Wait for cluster state to be consistent
		if successInRow >= minSuccessesInRow {
			log.Infof("cluster %s has status %s", clusterID, state)
			return
		}

		time.Sleep(time.Second)
	}

	ExpectWithOffset(1, lastState).Should(Equal(state), fmt.Sprintf("Cluster %s wasn't in state %s for %d times in a row. Actual %s (%s)",
		clusterID, state, minSuccessesInRow, lastState, lastStatusInfo))
}

func isHostInState(ctx context.Context, infraEnvID strfmt.UUID, hostID strfmt.UUID, state string) (bool, string, string) {
	rep, err := userBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
		InfraEnvID: infraEnvID,
		HostID:     hostID,
	})
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	h := rep.GetPayload()
	return swag.StringValue(h.Status) == state, swag.StringValue(h.Status), swag.StringValue(h.StatusInfo)
}

func waitForHostState(ctx context.Context, state string, timeout time.Duration, hosts ...*models.Host) {
	waitForHost := func(host *models.Host) error {
		log.Infof("Waiting for host %s state %s", host.ID.String(), state)
		var (
			lastState      string
			lastStatusInfo string
			success        bool
		)

		for start, successInRow := time.Now(), 0; time.Since(start) < timeout; {
			success, lastState, lastStatusInfo = isHostInState(ctx, host.InfraEnvID, *host.ID, state)

			if success {
				successInRow++
			} else {
				successInRow = 0
			}

			// Wait for host state to be consistent
			if successInRow >= minSuccessesInRow {
				log.Infof("host %s has status %s", host.ID.String(), state)
				return nil
			}

			time.Sleep(time.Second)
		}

		if lastState != state {
			return fmt.Errorf("Host %s wasn't in state %s for %d times in a row. Actual %s (%s)",
				host.ID.String(), state, minSuccessesInRow, lastState, lastStatusInfo)
		}

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	errs, _ := errgroup.WithContext(ctx)
	defer cancel()

	for idx := range hosts {
		host := hosts[idx]
		errs.Go(func() error {
			return waitForHost(host)
		})
	}

	ExpectWithOffset(1, errs.Wait()).ShouldNot(HaveOccurred())
}

func waitForMachineNetworkCIDR(
	ctx context.Context, clusterID strfmt.UUID, machineNetworkCIDR string, timeout time.Duration) error {

	log.Infof("Waiting for cluster=%s to have machineNetworkCIDR=%s", clusterID, machineNetworkCIDR)

	currentMachineNetworkCIDR := ""
	for start, _ := time.Now(), 0; time.Since(start) < timeout; {
		rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()

		if machineNetworkCIDR == "" && !network.IsMachineCidrAvailable(&common.Cluster{Cluster: *c}) {
			return nil
		}

		if network.IsMachineCidrAvailable(&common.Cluster{Cluster: *c}) {
			currentMachineNetworkCIDR = string(c.MachineNetworks[0].Cidr)

			if currentMachineNetworkCIDR == machineNetworkCIDR {
				log.Infof("cluster=%s has machineNetworkCIDR=%s", clusterID, machineNetworkCIDR)
				return nil
			}
		}

		time.Sleep(time.Second)
	}

	return fmt.Errorf("cluster=%s has machineNetworkCIDR=%s but expected=%s",
		clusterID, currentMachineNetworkCIDR, machineNetworkCIDR)
}

func installCluster(clusterID strfmt.UUID) *models.Cluster {
	ctx := context.Background()
	reply, err := userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := reply.GetPayload()
	Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
	generateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)

	waitForClusterState(ctx, clusterID, models.ClusterStatusInstalling,
		180*time.Second, "Installation in progress")

	waitForHostState(ctx, models.HostStatusInstalling, defaultWaitForHostStateTimeout, c.Hosts...)

	rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c = rep.GetPayload()
	Expect(c).NotTo(BeNil())

	return c
}

func tryInstallClusterWithDiskResponses(clusterID strfmt.UUID, successfulHosts, failedHosts []*models.Host) *models.Cluster {
	Expect(len(failedHosts)).To(BeNumerically(">", 0))
	ctx := context.Background()
	reply, err := userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := reply.GetPayload()
	Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
	generateFailedDiskSpeedResponses(ctx, sdbId, failedHosts...)
	generateSuccessfulDiskSpeedResponses(ctx, sdbId, successfulHosts...)

	waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient,
		180*time.Second, IgnoreStateInfo)
	waitForHostState(ctx, models.HostStatusInsufficient, defaultWaitForHostStateTimeout, failedHosts...)

	expectedKnownHosts := make([]*models.Host, 0)
outer:
	for _, h := range c.Hosts {
		for _, fh := range failedHosts {
			if h.ID.String() == fh.ID.String() {
				continue outer
			}
		}
		expectedKnownHosts = append(expectedKnownHosts, h)
	}

	waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, expectedKnownHosts...)

	rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c = rep.GetPayload()
	Expect(c).NotTo(BeNil())

	return c
}

func completeInstallation(client *client.AssistedInstall, clusterID strfmt.UUID) {
	ctx := context.Background()
	rep, err := client.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())

	status := models.OperatorStatusAvailable

	Eventually(func() error {
		_, err = agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: models.IngressCertParams(ingressCa),
		})
		return err
	}, "10s", "2s").Should(BeNil())

	for _, operator := range rep.Payload.MonitoredOperators {
		if operator.OperatorType != models.OperatorTypeBuiltin {
			continue
		}

		v2ReportMonitoredOperatorStatus(ctx, client, clusterID, operator.Name, status, "")
	}
}

func failInstallation(client *client.AssistedInstall, clusterID strfmt.UUID) {
	ctx := context.Background()
	isSuccess := false
	_, err := client.Installer.V2CompleteInstallation(ctx, &installer.V2CompleteInstallationParams{
		ClusterID: clusterID,
		CompletionParams: &models.CompletionParams{
			IsSuccess: &isSuccess,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

func completeInstallationAndVerify(ctx context.Context, client *client.AssistedInstall, clusterID strfmt.UUID, completeSuccess bool) {
	if completeSuccess {
		completeInstallation(client, clusterID)
		waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, IgnoreStateInfo)
	} else {
		failInstallation(client, clusterID)
	}
}

func setClusterAsInstalling(ctx context.Context, clusterID strfmt.UUID) {
	c := installCluster(clusterID)
	Expect(swag.StringValue(c.Status)).Should(Equal("installing"))
	Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))

	for _, host := range c.Hosts {
		Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
	}
}

func setClusterAsFinalizing(ctx context.Context, clusterID strfmt.UUID) {
	setClusterAsInstalling(ctx, clusterID)
	c := getCluster(clusterID)

	for _, host := range c.Hosts {
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
	}

	waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
}

var _ = Describe("V2ListClusters", func() {

	var (
		ctx     = context.Background()
		cluster *models.Cluster
	)

	BeforeEach(func() {

		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:     "example.com",
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String(VipAutoAllocOpenshiftVersion),
				PullSecret:        swag.String(pullSecret),
				SSHPublicKey:      sshPublicKey,
				VipDhcpAllocation: swag.Bool(true),
				NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		log.Infof("Register cluster %s", cluster.ID.String())
	})

	Context("Filter by opensfhift cluster ID", func() {

		BeforeEach(func() {
			infraEnvID := registerInfraEnv(cluster.ID, models.ImageTypeMinimalIso).ID
			registerHostsAndSetRolesDHCP(*cluster.ID, *infraEnvID, 5, "test-cluster", "example.com")
			_ = installCluster(*cluster.ID)
		})

		It("searching for an existing openshift cluster ID", func() {
			list, err := userBMClient.Installer.V2ListClusters(
				ctx,
				&installer.V2ListClustersParams{OpenshiftClusterID: strToUUID("41940ee8-ec99-43de-8766-174381b4921d")})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("discarding openshift cluster ID field", func() {
			list, err := userBMClient.Installer.V2ListClusters(
				ctx,
				&installer.V2ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("searching for a non-existing openshift cluster ID", func() {
			list, err := userBMClient.Installer.V2ListClusters(
				ctx,
				&installer.V2ListClustersParams{OpenshiftClusterID: strToUUID("00000000-0000-0000-0000-000000000000")})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(0))
		})
	})

	Context("Filter by AMS subscription IDs", func() {

		BeforeEach(func() {
			if Options.AuthType == auth.TypeNone {
				Skip("auth is disabled")
			}
		})

		It("searching for an existing AMS subscription ID", func() {
			list, err := userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{
				AmsSubscriptionIds: []string{FakeSubscriptionID.String()},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))

		})

		It("discarding AMS subscription ID field", func() {
			list, err := userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("searching for a non-existing AMS Subscription ID", func() {
			list, err := userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{
				AmsSubscriptionIds: []string{"1h89fvtqeelulpo0fl5oddngj2ao7XXX"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(0))
		})

		It("searching for both existing and non-existing AMS subscription IDs", func() {
			list, err := userBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{
				AmsSubscriptionIds: []string{
					FakeSubscriptionID.String(),
					"1h89fvtqeelulpo0fl5oddngj2ao7XXX",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))

		})
	})
})

var _ = Describe("cluster install - DHCP", func() {
	var (
		ctx         = context.Background()
		cluster     *models.Cluster
		infraEnvID  *strfmt.UUID
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)

	generateDhcpStepReply := func(h *models.Host, apiVip, ingressVip string, errorExpected bool) {
		avip := strfmt.IPv4(apiVip)
		ivip := strfmt.IPv4(ingressVip)
		r := models.DhcpAllocationResponse{
			APIVipAddress:     &avip,
			IngressVipAddress: &ivip,
		}
		b, err := json.Marshal(&r)
		Expect(err).ToNot(HaveOccurred())
		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: h.InfraEnvID,
			HostID:     *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				StepType: models.StepTypeDhcpLeaseAllocate,
				Output:   string(b),
				StepID:   string(models.StepTypeDhcpLeaseAllocate),
			},
		})
		if errorExpected {
			ExpectWithOffset(1, err).Should(HaveOccurred())
		} else {
			ExpectWithOffset(1, err).ShouldNot(HaveOccurred())
		}
	}

	BeforeEach(func() {

		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:     "example.com",
				ClusterNetworks:   []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:   []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String(VipAutoAllocOpenshiftVersion),
				PullSecret:        swag.String(pullSecret),
				SSHPublicKey:      sshPublicKey,
				VipDhcpAllocation: swag.Bool(true),
				NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		log.Infof("Register cluster %s", cluster.ID.String())
	})
	Context("install cluster cases", func() {
		var clusterID strfmt.UUID

		BeforeEach(func() {
			clusterID = *cluster.ID
			infraEnvID = registerInfraEnvSpecificVersion(&clusterID, models.ImageTypeMinimalIso, cluster.OpenshiftVersion).ID
			registerHostsAndSetRolesDHCP(clusterID, *infraEnvID, 5, "test-cluster", "example.com")
		})

		It("Install with DHCP", func() {
			c := installCluster(clusterID)
			Expect(len(c.Hosts)).Should(Equal(5))

			var atLeastOneBootstrap = false

			for _, h := range c.Hosts {
				if h.Bootstrap {
					Expect(h.ProgressStages).Should(Equal(host.BootstrapStages[:]))
					atLeastOneBootstrap = true
				} else if h.Role == models.HostRoleMaster {
					Expect(h.ProgressStages).Should(Equal(host.MasterStages[:]))
				} else {
					Expect(h.ProgressStages).Should(Equal(host.WorkerStages[:]))
				}
			}

			Expect(atLeastOneBootstrap).Should(BeTrue())
		})

		AfterEach(func() {
			reply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			Expect(reply.GetPayload().OpenshiftClusterID).To(Equal(*strToUUID("41940ee8-ec99-43de-8766-174381b4921d")))
		})
	})

	It("moves between DHCP modes", func() {
		clusterID := *cluster.ID
		infraEnvID = registerInfraEnvSpecificVersion(&clusterID, models.ImageTypeMinimalIso, cluster.OpenshiftVersion).ID
		registerHostsAndSetRolesDHCP(clusterID, *infraEnvID, 5, "test-cluster", "example.com")
		reply, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(swag.StringValue(reply.Payload.Status)).To(Equal(models.ClusterStatusPendingForInput))

		for i := range reply.Payload.Hosts {
			Expect(reply.Payload.Hosts[i].RequestedHostname).Should(Not(BeEmpty()))
		}

		generateDhcpStepReply(reply.Payload.Hosts[0], "1.2.3.102", "1.2.3.103", true)
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				APIVips:     []*models.APIVip{{IP: "1.2.3.100", ClusterID: clusterID}},
				IngressVips: []*models.IngressVip{{IP: "1.2.3.101", ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
		reply, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(swag.StringValue(reply.Payload.Status)).To(Equal(models.ClusterStatusReady))
		reply, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(true),
				MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				APIVips:     []*models.APIVip{{IP: "1.2.3.100", ClusterID: clusterID}},
				IngressVips: []*models.IngressVip{{IP: "1.2.3.101", ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
		generateDhcpStepReply(reply.Payload.Hosts[0], "1.2.3.102", "1.2.3.103", false)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)
		getReply, err := userBMClient.Installer.V2GetCluster(ctx, installer.NewV2GetClusterParams().WithClusterID(clusterID))
		Expect(err).ToNot(HaveOccurred())
		c := getReply.Payload
		Expect(swag.StringValue(c.Status)).To(Equal(models.ClusterStatusReady))
		Expect(string(c.APIVips[0].IP)).To(Equal("1.2.3.102"))
		Expect(string(c.IngressVips[0].IP)).To(Equal("1.2.3.103"))
	})
})

var _ = Describe("Validate BaseDNSDomain when creating a cluster", func() {
	var (
		ctx         = context.Background()
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)
	type DNSTest struct {
		It            string
		BaseDNSDomain string
		ShouldThrow   bool
	}
	createClusterWithBaseDNS := func(baseDNS string) (*installer.V2RegisterClusterCreated, error) {
		return userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    baseDNS,
				ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
	}
	tests := []DNSTest{
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='example', valid DNS",
			BaseDNSDomain: "example",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should throw an error. BaseDNSDomain='example.c', Invalid top-level domain name ",
			BaseDNSDomain: "example.c",
			ShouldThrow:   true,
		},
		{
			It:            "V2RegisterCluster should throw an error. BaseDNSDomain='-example.com', Illegal first character in domain name",
			BaseDNSDomain: "-example.com",
			ShouldThrow:   true,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='1-example.com', valid DNS",
			BaseDNSDomain: "1-example.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='example.com', valid DNS",
			BaseDNSDomain: "example.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='sub.example.com', valid DNS",
			BaseDNSDomain: "sub.example.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='deep.sub.example.com', valid DNS",
			BaseDNSDomain: "deep.sub.example.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='exam-ple.com', valid DNS",
			BaseDNSDomain: "exam-ple.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='exam--ple.com', valid DNS",
			BaseDNSDomain: "exam--ple.com",
			ShouldThrow:   false,
		},
		{
			It:            "V2RegisterCluster should not throw an error. BaseDNSDomain='example.com1', valid DNS",
			BaseDNSDomain: "example.com1",
			ShouldThrow:   false,
		},
	}
	for _, test := range tests {
		t := test
		It(test.It, func() {
			_, err := createClusterWithBaseDNS(t.BaseDNSDomain)
			if t.ShouldThrow {
				Expect(err).Should(HaveOccurred())
			} else {
				Expect(err).ShouldNot(HaveOccurred())
			}
		})
	}
})

var _ = Describe("cluster update - BaseDNS", func() {
	var (
		ctx         = context.Background()
		cluster     *models.Cluster
		clusterID   strfmt.UUID
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
		err         error
	)

	BeforeEach(func() {
		var registerClusterReply *installer.V2RegisterClusterCreated
		registerClusterReply, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		clusterID = *cluster.ID
		log.Infof("Register cluster %s", cluster.ID.String())
	})
	Context("Update BaseDNS", func() {
		It("Should not throw an error with valid 2 part DNS", func() {
			_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					BaseDNSDomain: swag.String("abc.com"),
				},
				ClusterID: clusterID,
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("Should not throw an error with valid 3 part DNS", func() {
			_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					BaseDNSDomain: swag.String("abc.def.com"),
				},
				ClusterID: clusterID,
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
	It("Should throw an error with invalid top-level domain", func() {
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				BaseDNSDomain: swag.String("abc.com.c"),
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
	})
	It("Should throw an error with invalid char prefix domain", func() {
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				BaseDNSDomain: swag.String("-abc.com"),
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("cluster install", func() {
	var (
		ctx           = context.Background()
		cluster       *models.Cluster
		infraEnvID    *strfmt.UUID
		clusterCIDR   = "10.128.0.0/14"
		serviceCIDR   = "172.30.0.0/16"
		machineCIDR   = "1.2.3.0/24"
		clusterCIDRv6 = "1003:db8::/53"
		serviceCIDRv6 = "1002:db8::/119"
		machineCIDRv6 = "1001:db8::/120"
	)

	BeforeEach(func() {
		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		log.Infof("Register cluster %s", cluster.ID.String())
		infraEnvID = registerInfraEnv(cluster.ID, models.ImageTypeMinimalIso).ID
	})
	AfterEach(func() {
		deregisterResources()
		clearDB()
	})
	getSuggestedRole := func(id strfmt.UUID) models.HostRole {
		reply, err := userBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     id,
		})
		Expect(err).ShouldNot(HaveOccurred())
		return reply.GetPayload().SuggestedRole
	}
	It("auto-assign", func() {
		By("register 3 hosts all with master hw information cluster expected to be ready")
		clusterID := *cluster.ID
		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		h1, h2, h3 := hosts[0], hosts[1], hosts[2]
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("change first host hw info to worker and expect the cluster to become insufficient")
		generateHWPostStepReply(ctx, h1, getValidWorkerHwInfoWithCIDR(ips[0]), "h1")
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("add two more hosts with minimal master inventory expect the cluster to be ready")
		newIPs := hostutil.GenerateIPv4Addresses(3, ips[2])
		h4 := &registerHost(*infraEnvID).Host
		generateEssentialHostStepsWithInventory(ctx, h4, "h4", getMinimalMasterInventory(newIPs[0]))

		h5 := &registerHost(*infraEnvID).Host
		generateEssentialHostStepsWithInventory(ctx, h5, "h5", getMinimalMasterInventory(newIPs[1]))

		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5)
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h4, h5)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("expect h4 and h5 to be auto-assigned as masters")
		Expect(getSuggestedRole(*h4.ID)).Should(Equal(models.HostRoleMaster))
		Expect(getSuggestedRole(*h5.ID)).Should(Equal(models.HostRoleMaster))

		By("add hosts with worker inventory expect the cluster to be ready")
		h6 := &registerHost(*infraEnvID).Host
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		generateEssentialHostStepsWithInventory(ctx, h6, "h6", getValidWorkerHwInfoWithCIDR(newIPs[2]))
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5, h6)
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h6)

		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("start installation and validate roles")
		_, err := userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		generateEssentialPrepareForInstallationSteps(ctx, h1, h2, h3, h4, h5, h6)
		waitForClusterState(context.Background(), clusterID, models.ClusterStatusInstalling,
			3*time.Minute, IgnoreStateInfo)

		getHostRole := func(id strfmt.UUID) models.HostRole {
			var reply *installer.V2GetHostOK
			reply, err = userBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
				InfraEnvID: *infraEnvID,
				HostID:     id,
			})
			Expect(err).ShouldNot(HaveOccurred())
			return reply.GetPayload().Role
		}
		Expect(getHostRole(*h1.ID)).Should(Equal(models.HostRoleWorker))
		Expect(getHostRole(*h6.ID)).Should(Equal(models.HostRoleWorker))
		Expect(getHostRole(*h4.ID)).Should(Equal(models.HostRoleMaster))
		Expect(getHostRole(*h5.ID)).Should(Equal(models.HostRoleMaster))
		getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		mastersCount := 0
		workersCount := 0
		for _, h := range getReply.GetPayload().Hosts {
			if h.Role == models.HostRoleMaster {
				mastersCount++
			}
			if h.Role == models.HostRoleWorker {
				workersCount++
			}
		}
		Expect(mastersCount).Should(Equal(3))
		Expect(workersCount).Should(Equal(3))

		By("check auto-assign usage report")
		verifyUsageSet(getReply.Payload.FeatureUsage,
			models.Usage{Name: usage.AutoAssignRoleUsage})
	})

	//TODO: stabilize this test
	It("auto-assign_with_cnv_operator", func() {
		By("register 3 hosts all with master hw information and virtualization, cluster expected to be ready")
		clusterID := *cluster.ID
		ips := hostutil.GenerateIPv4Addresses(6, defaultCIDRv4)
		hosts := make([]*models.Host, 6)

		for i := 0; i < 3; i++ {
			hwInventory := getDefaultInventory(ips[i])
			hwInventory.CPU.Flags = []string{"vmx"}
			hosts[i] = &registerHost(*infraEnvID).Host
			generateEssentialHostStepsWithInventory(ctx, hosts[i], fmt.Sprintf("hhh%d", i+1), hwInventory)
		}
		updateVipParams(ctx, clusterID)
		generateFullMeshConnectivity(ctx, ips[0], hosts[0], hosts[1], hosts[2])

		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, hosts[0], hosts[1], hosts[2])
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("add three more hosts with minimal master inventory expect the cluster to be ready")
		for i := 3; i < 6; i++ {
			minHwInventory := getMinimalMasterInventory(ips[i])
			minHwInventory.CPU.Flags = []string{"vmx"}
			hosts[i] = &registerHost(*infraEnvID).Host
			generateEssentialHostStepsWithInventory(ctx, hosts[i], fmt.Sprintf("hhh%d", i+1), minHwInventory)
		}
		generateFullMeshConnectivity(ctx, ips[0], hosts...)
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, hosts...)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("expect h4, h5 and h6 to be auto-assigned as masters")
		Expect(getSuggestedRole(*hosts[3].ID)).Should(Equal(models.HostRoleMaster))
		Expect(getSuggestedRole(*hosts[4].ID)).Should(Equal(models.HostRoleMaster))
		Expect(getSuggestedRole(*hosts[5].ID)).Should(Equal(models.HostRoleMaster))

		By("add cnv operators")
		_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterID: clusterID,
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				OlmOperators: []*models.OperatorCreateParams{
					{Name: cnv.Operator.Name},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		waitForHostState(ctx, models.HostStatusKnown, 3*defaultWaitForHostStateTimeout, hosts...)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("expect h1, h2 and h3 to be auto-assigned as masters")
		Expect(getSuggestedRole(*hosts[0].ID)).Should(Equal(models.HostRoleMaster))
		Expect(getSuggestedRole(*hosts[1].ID)).Should(Equal(models.HostRoleMaster))
		Expect(getSuggestedRole(*hosts[2].ID)).Should(Equal(models.HostRoleMaster))
	})

	It("Schedulable masters", func() {
		By("register 3 hosts all with master hw information cluster expected to be ready")
		clusterID := *cluster.ID
		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		h1, h2, h3 := hosts[0], hosts[1], hosts[2]
		for _, h := range hosts {
			generateDomainResolution(ctx, h, "test-cluster", "example.com")
		}
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("add two more hosts with worker inventory expect the cluster to be ready")
		h4 := &registerHost(*infraEnvID).Host
		h5 := &registerHost(*infraEnvID).Host
		newIPs := hostutil.GenerateIPv4Addresses(2, ips[2])
		generateEssentialHostStepsWithInventory(ctx, h4, "h4", getValidWorkerHwInfoWithCIDR(newIPs[0]))
		generateEssentialHostStepsWithInventory(ctx, h5, "h5", getValidWorkerHwInfoWithCIDR(newIPs[1]))
		generateDomainResolution(ctx, h4, "test-cluster", "example.com")
		generateDomainResolution(ctx, h5, "test-cluster", "example.com")
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5)
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h4, h5)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		updateClusterReply, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				SchedulableMasters: swag.Bool(true),
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(*updateClusterReply.GetPayload().SchedulableMasters).Should(BeTrue())
		for i := range updateClusterReply.Payload.Hosts {
			Expect(updateClusterReply.Payload.Hosts[i].RequestedHostname).Should(Not(BeEmpty()))
		}

		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("start installation")
		_, err = userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		generateEssentialPrepareForInstallationSteps(ctx, h1, h2, h3, h4, h5)
		waitForClusterState(context.Background(), clusterID, models.ClusterStatusInstalling,
			3*time.Minute, IgnoreStateInfo)
	})

	Context("usage", func() {
		It("report usage on default features with SNO", func() {
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:        "example.com",
					ClusterNetworks:      []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:      []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:                 swag.String("sno-cluster"),
					OpenshiftVersion:     swag.String(snoVersion),
					PullSecret:           swag.String(pullSecret),
					SSHPublicKey:         sshPublicKey,
					VipDhcpAllocation:    swag.Bool(false),
					NetworkType:          swag.String("OVNKubernetes"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster = registerClusterReply.GetPayload()
			getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: *cluster.ID})
			Expect(err).NotTo(HaveOccurred())
			log.Infof("usage after create: %s\n", getReply.Payload.FeatureUsage)
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.HighAvailabilityModeUsage},
				models.Usage{Name: usage.HyperthreadingUsage, Data: map[string]interface{}{"hyperthreading_enabled": models.ClusterHyperthreadingAll}})
			verifyUsageNotSet(getReply.Payload.FeatureUsage,
				strings.ToUpper("console"),
				usage.VipDhcpAllocationUsage,
				usage.CPUArchitectureARM64,
				usage.SDNNetworkTypeUsage,
				usage.DualStackUsage,
				usage.DualStackVipsUsage)
		})

		It("report usage on update cluster", func() {
			clusterID := *cluster.ID
			h := &registerHost(*infraEnvID).Host
			inventory, err := common.UnmarshalInventory(defaultInventory())
			Expect(err).ToNot(HaveOccurred())
			inventory.SystemVendor.Virtual = true
			inventoryStr, err := common.MarshalInventory(inventory)
			Expect(err).ToNot(HaveOccurred())
			h = updateInventory(ctx, *infraEnvID, *h.ID, inventoryStr)
			ntpSources := "1.1.1.1,2.2.2.2"
			proxy := "http://1.1.1.1:8080"
			no_proxy := "a.redhat.com"
			ovn := "OVNKubernetes"
			hostname := "h1"
			hyperthreading := "none"
			_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
				HostUpdateParams: &models.HostUpdateParams{
					HostName: &hostname,
				},
				HostID:     *h.ID,
				InfraEnvID: h.InfraEnvID,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation:     swag.Bool(false),
					AdditionalNtpSource:   &ntpSources,
					HTTPProxy:             &proxy,
					HTTPSProxy:            &proxy,
					NoProxy:               &no_proxy,
					NetworkType:           &ovn,
					UserManagedNetworking: swag.Bool(false),
					Hyperthreading:        &hyperthreading,
				},
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			verifyUsageNotSet(getReply.Payload.FeatureUsage,
				usage.SDNNetworkTypeUsage,
				usage.DualStackUsage,
				usage.DualStackVipsUsage,
				usage.HyperthreadingUsage)
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.OVNNetworkTypeUsage},
				models.Usage{
					Name: usage.ClusterManagedNetworkWithVMs,
					Data: map[string]interface{}{
						"VM Hosts": []interface{}{
							h.ID.String(),
						},
					},
				},
				models.Usage{
					Name: usage.AdditionalNtpSourceUsage,
					Data: map[string]interface{}{
						"source_count": 2.0,
					},
				},
				models.Usage{
					Name: usage.ProxyUsage,
					Data: map[string]interface{}{
						"http_proxy":  1.0,
						"https_proxy": 1.0,
						"no_proxy":    1.0,
					},
				},
				models.Usage{
					Name: usage.RequestedHostnameUsage,
					Data: map[string]interface{}{
						"host_count": 1.0,
					},
				},
			)
		})
	})

	Context("dual-stack usage", func() {
		It("report usage new dual-stack cluster", func() {
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					APIVips:       []*models.APIVip{{IP: "1.2.3.8"}},
					IngressVips:   []*models.IngressVip{{IP: "1.2.3.9"}},
					BaseDNSDomain: "example.com",
					ClusterNetworks: []*models.ClusterNetwork{
						{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23},
						{Cidr: models.Subnet(clusterCIDRv6), HostPrefix: 64}},
					ServiceNetworks: []*models.ServiceNetwork{
						{Cidr: models.Subnet(serviceCIDR)},
						{Cidr: models.Subnet(serviceCIDRv6)}},
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: models.Subnet(machineCIDR)},
						{Cidr: models.Subnet(machineCIDRv6)}},
					Name:                 swag.String("sno-cluster"),
					OpenshiftVersion:     swag.String(snoVersion),
					PullSecret:           swag.String(pullSecret),
					SSHPublicKey:         sshPublicKey,
					VipDhcpAllocation:    swag.Bool(false),
					NetworkType:          swag.String("OVNKubernetes"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster = registerClusterReply.GetPayload()
			getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: *cluster.ID})
			Expect(err).NotTo(HaveOccurred())
			log.Infof("usage after create: %s\n", getReply.Payload.FeatureUsage)
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.HighAvailabilityModeUsage},
				models.Usage{Name: usage.DualStackUsage})
			verifyUsageNotSet(getReply.Payload.FeatureUsage,
				strings.ToUpper("console"),
				usage.VipDhcpAllocationUsage,
				usage.CPUArchitectureARM64,
				usage.SDNNetworkTypeUsage,
				usage.DualStackVipsUsage)
		})

		It("report usage new dual-stack cluster with dual-stack VIPs", func() {
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					APIVips:       []*models.APIVip{{IP: "1.2.3.8"}, {IP: "1001:db8::8"}},
					IngressVips:   []*models.IngressVip{{IP: "1.2.3.9"}, {IP: "1001:db8::9"}},
					BaseDNSDomain: "example.com",
					ClusterNetworks: []*models.ClusterNetwork{
						{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23},
						{Cidr: models.Subnet(clusterCIDRv6), HostPrefix: 64}},
					ServiceNetworks: []*models.ServiceNetwork{
						{Cidr: models.Subnet(serviceCIDR)},
						{Cidr: models.Subnet(serviceCIDRv6)}},
					MachineNetworks: []*models.MachineNetwork{
						{Cidr: models.Subnet(machineCIDR)},
						{Cidr: models.Subnet(machineCIDRv6)}},
					Name:                 swag.String("sno-cluster"),
					OpenshiftVersion:     swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:           swag.String(pullSecret),
					SSHPublicKey:         sshPublicKey,
					VipDhcpAllocation:    swag.Bool(false),
					NetworkType:          swag.String("OVNKubernetes"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster = registerClusterReply.GetPayload()
			getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: *cluster.ID})
			Expect(err).NotTo(HaveOccurred())
			log.Infof("usage after create: %s\n", getReply.Payload.FeatureUsage)
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.HighAvailabilityModeUsage},
				models.Usage{Name: usage.DualStackUsage},
				models.Usage{Name: usage.DualStackVipsUsage})
			verifyUsageNotSet(getReply.Payload.FeatureUsage,
				strings.ToUpper("console"),
				usage.VipDhcpAllocationUsage,
				usage.CPUArchitectureARM64,
				usage.SDNNetworkTypeUsage)
		})

		It("unset dual-stack usage on update cluster", func() {
			clusterID := *cluster.ID
			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation:     swag.Bool(false),
					NetworkType:           swag.String("OVNKubernetes"),
					UserManagedNetworking: swag.Bool(false),
					ClusterNetworks:       []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:       []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					MachineNetworks:       []*models.MachineNetwork{},
				},
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			getReply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			verifyUsageNotSet(getReply.Payload.FeatureUsage,
				usage.VipDhcpAllocationUsage,
				usage.SDNNetworkTypeUsage,
				usage.DualStackUsage,
				usage.DualStackVipsUsage)
		})
	})

	Context("MachineNetworkCIDR auto assign", func() {

		It("MachineNetworkCIDR successful allocating", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			ingressVip := "1.2.3.100"
			_ = registerNode(ctx, *infraEnvID, "test-host", defaultCIDRv4)
			c, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
					IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(c.GetPayload().Hosts)).Should(Equal(1))
			Expect(string(c.Payload.APIVips[0].IP)).Should(Equal(apiVip))
			Expect(string(c.Payload.IngressVips[0].IP)).To(Equal("1.2.3.100"))
			Expect(string(c.Payload.MachineNetworks[0].Cidr)).Should(Equal("1.2.3.0/24"))
		})

		It("MachineNetworkCIDR successful deallocating ", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			ingressVip := "1.2.3.100"
			host := registerNode(ctx, *infraEnvID, "test-host", defaultCIDRv4)
			c, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
					IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(string(c.Payload.APIVips[0].IP)).Should(Equal(apiVip))
			Expect(string(c.Payload.IngressVips[0].IP)).To(Equal(ingressVip))
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).ShouldNot(HaveOccurred())
			_, err1 := userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
				InfraEnvID: *infraEnvID,
				HostID:     *host.ID,
			})
			Expect(err1).ShouldNot(HaveOccurred())
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "", defaultWaitForMachineNetworkCIDRTimeout)).ShouldNot(HaveOccurred())
		})

		It("MachineNetworkCIDR no vips - no allocation", func() {
			clusterID := *cluster.ID
			c, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(c.GetPayload().Hosts)).Should(Equal(0))
			Expect(len(c.Payload.APIVips)).Should(Equal(0))
			Expect(len(c.Payload.IngressVips)).Should(Equal(0))
			Expect(c.Payload.MachineNetworks).Should(BeEmpty())
			_ = registerNode(ctx, *infraEnvID, "test-host", defaultCIDRv4)
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).Should(HaveOccurred())
			c1, err1 := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err1).NotTo(HaveOccurred())
			Expect(c1.Payload.MachineNetworks).Should(BeEmpty())
		})

		It("MachineNetworkCIDR no hosts - no allocation", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).To(HaveOccurred())
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).Should(HaveOccurred())
			c1, err1 := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err1).NotTo(HaveOccurred())
			Expect(c1.Payload.MachineNetworks).Should(BeEmpty())
		})
	})

	Context("wait until all hosts are done", func() {
		var (
			clusterID         strfmt.UUID
			misbehavingHostID strfmt.UUID
		)
		BeforeEach(func() {
			clusterID = *cluster.ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, 6, cluster.Name, cluster.BaseDNSDomain)
			cluster = getCluster(clusterID)
			for _, h := range cluster.Hosts {
				if h.Role == models.HostRoleWorker {
					misbehavingHostID = *h.ID
					break
				}
			}
		})
		stages := []models.HostStage{models.HostStageFailed, models.HostStageDone}
		for i := range stages {
			stage := stages[i]
			It(fmt.Sprintf("full flow with single host in stage %s", string(stage)), func() {
				By("installing cluster", func() {
					installCluster(clusterID)
					cluster = getCluster(clusterID)
				})

				By("move cluster to finalizing", func() {
					for _, h := range cluster.Hosts {
						if *h.ID != misbehavingHostID {
							updateProgress(*h.ID, h.InfraEnvID, models.HostStageDone)
						} else {
							updateProgress(*h.ID, h.InfraEnvID, models.HostStageRebooting)
						}
					}
					waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout,
						IgnoreStateInfo)
				})

				By("complete installation. state should be still finalizing", func() {
					completeInstallation(agentBMClient, clusterID)
					waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout,
						IgnoreStateInfo)
				})

				By("register host. move to pending-user-action", func() {
					c1 := getCluster(clusterID)
					_ = registerHostByUUID(*infraEnvID, misbehavingHostID)
					waitForClusterState(ctx, clusterID, models.ClusterStatusInstallingPendingUserAction, defaultWaitForClusterStateTimeout,
						IgnoreStateInfo)
					host := getHostV2(*infraEnvID, misbehavingHostID)
					Expect(swag.StringValue(host.Status)).To(Equal(models.HostStatusInstallingPendingUserAction))
					c2 := getCluster(clusterID)
					Expect(c1.Progress.TotalPercentage).To(BeNumerically("<=", c2.Progress.TotalPercentage))
				})
				By("move to configuring. cluster should be back in finalizing", func() {
					updateProgress(misbehavingHostID, *infraEnvID, models.HostStageConfiguring)
					waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout,
						IgnoreStateInfo)
				})
				By(fmt.Sprintf("update host to to stage %s.  Cluster should be installed", string(stage)), func() {
					updateProgress(misbehavingHostID, *infraEnvID, stage)
					waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout,
						IgnoreStateInfo)
				})
			})
		}
	})

	Context("install cluster cases", func() {
		var clusterID strfmt.UUID
		BeforeEach(func() {
			clusterID = *cluster.ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
		})

		Context("NTP cases", func() {
			It("Update NTP source", func() {
				c, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				hosts := c.GetPayload().Hosts

				By("Verify NTP step", func() {
					step := getStepFromListByStepType(getNextSteps(*infraEnvID, *hosts[0].ID), models.StepTypeNtpSynchronizer)
					Expect(step).ShouldNot(BeNil())

					requestStr := step.Args[len(step.Args)-1]
					var ntpRequest models.NtpSynchronizationRequest

					Expect(json.Unmarshal([]byte(requestStr), &ntpRequest)).ShouldNot(HaveOccurred())
					Expect(*ntpRequest.NtpSource).Should(Equal(c.Payload.AdditionalNtpSource))
				})

				By("Update NTP source", func() {
					newSource := "5.5.5.5"

					reply, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &newSource,
						},
						ClusterID: clusterID,
					})
					Expect(err).ShouldNot(HaveOccurred())
					Expect(reply.Payload.AdditionalNtpSource).Should(Equal(newSource))

					step := getStepFromListByStepType(getNextSteps(*infraEnvID, *hosts[0].ID), models.StepTypeNtpSynchronizer)
					Expect(step).ShouldNot(BeNil())

					requestStr := step.Args[len(step.Args)-1]
					var ntpRequest models.NtpSynchronizationRequest

					generateDomainNameResolutionReply(ctx, hosts[0], *common.TestDomainNameResolutionsSuccess)

					Expect(json.Unmarshal([]byte(requestStr), &ntpRequest)).ShouldNot(HaveOccurred())
					Expect(*ntpRequest.NtpSource).Should(Equal(newSource))
				})
			})

			It("Unsynced host", func() {
				Skip("IsNTPSynced isn't mandatory validation for host isSufficientForInstall")

				c, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				hosts := c.GetPayload().Hosts

				By("unsync", func() {
					generateNTPPostStepReply(ctx, hosts[0], []*models.NtpSource{
						{SourceName: common.TestNTPSourceSynced.SourceName, SourceState: models.SourceStateUnreachable},
					})
					generateDomainNameResolutionReply(ctx, hosts[0], *common.TestDomainNameResolutionsSuccess)
					waitForHostState(ctx, models.HostStatusInsufficient, defaultWaitForHostStateTimeout, hosts[0])
				})

				By("Set new NTP source", func() {
					newSource := "5.5.5.5"

					_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &newSource,
						},
						ClusterID: clusterID,
					})
					Expect(err).ShouldNot(HaveOccurred())

					generateNTPPostStepReply(ctx, hosts[0], []*models.NtpSource{
						{SourceName: common.TestNTPSourceSynced.SourceName, SourceState: models.SourceStateUnreachable},
						{SourceName: newSource, SourceState: models.SourceStateSynced},
					})
				})
				generateDomainNameResolutionReply(ctx, hosts[0], *common.TestDomainNameResolutionsSuccess)

				waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, hosts[0])
			})
		})

		It("register host while installing", func() {
			installCluster(clusterID)
			waitForClusterState(ctx, clusterID, models.ClusterStatusInstalling, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
			_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
				InfraEnvID: *infraEnvID,
				NewHostParams: &models.HostCreateParams{
					HostID: strToUUID(uuid.New().String()),
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterHostConflict()))
		})

		It("register host while cluster in error state", func() {
			FailCluster(ctx, clusterID, *infraEnvID, masterFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
			_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
				InfraEnvID: *infraEnvID,
				NewHostParams: &models.HostCreateParams{
					HostID: strToUUID(uuid.New().String()),
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterHostConflict()))
		})

		It("fail installation if there is only a single worker that manages to install", func() {
			FailCluster(ctx, clusterID, *infraEnvID, workerFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
		})

		It("register existing host while cluster in installing state", func() {
			c := installCluster(clusterID)
			hostID := c.Hosts[0].ID
			_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
				InfraEnvID: *infraEnvID,
				NewHostParams: &models.HostCreateParams{
					HostID: hostID,
				},
			})
			Expect(err).To(BeNil())
			host := getHostV2(*infraEnvID, *hostID)
			Expect(*host.Status).To(Equal("error"))
		})

		It("register host after reboot - wrong boot order", func() {
			c := installCluster(clusterID)
			hostID := c.Hosts[0].ID

			Expect(isStepTypeInList(getNextSteps(*infraEnvID, *hostID), models.StepTypeInstall)).Should(BeTrue())

			installProgress := models.HostStageRebooting
			updateProgress(*hostID, *infraEnvID, installProgress)

			By("Verify the db has been updated", func() {
				hostInDb := getHostV2(*infraEnvID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostInDb.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostInDb.InstallationDiskID).ShouldNot(BeEmpty())
				Expect(hostInDb.InstallationDiskPath).ShouldNot(BeEmpty())
				Expect(hostInDb.Inventory).ShouldNot(BeEmpty())
			})

			By("Try to register", func() {
				_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
					InfraEnvID: *infraEnvID,
					NewHostParams: &models.HostCreateParams{
						HostID: hostID,
					},
				})
				Expect(err).To(BeNil())
				hostInDb := getHostV2(*infraEnvID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingPendingUserAction))

				waitForClusterState(
					ctx,
					clusterID,
					models.ClusterStatusInstallingPendingUserAction,
					defaultWaitForClusterStateTimeout,
					clusterInstallingPendingUserActionStateInfo)
			})

			By("Updating progress after fixing boot order", func() {
				installProgress = models.HostStageConfiguring
				updateProgress(*hostID, *infraEnvID, installProgress)
			})

			By("Verify the db has been updated", func() {
				hostInDb := getHostV2(*infraEnvID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostInDb.StatusInfo).Should(Equal(string(installProgress)))
				waitForClusterState(
					ctx,
					clusterID,
					models.ClusterStatusInstalling,
					defaultWaitForClusterStateTimeout,
					clusterInstallingStateInfo)
			})
		})

		It("[minimal-set]install_cluster", func() {
			By("Installing cluster till finalize")
			setClusterAsFinalizing(ctx, clusterID)
			By("Completing installation installation")
			completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
		})

		It("install_cluster install command failed", func() {
			By("Installing cluster")
			c := installCluster(clusterID)
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalling))
			Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
			Expect(len(c.Hosts)).Should(Equal(5))
			var masterID strfmt.UUID
			for _, host := range c.Hosts {
				Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusInstalling))
				if masterID == "" && host.Role == models.HostRoleMaster {
					masterID = *host.ID
				}
			}

			// post failure to execute the install command
			_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
				InfraEnvID: *infraEnvID,
				HostID:     masterID,
				Reply: &models.StepReply{
					ExitCode: bminventory.ContainerAlreadyRunningExitCode,
					StepType: models.StepTypeInstall,
					Output:   "blabla",
					Error:    "Some random error",
					StepID:   string(models.StepTypeInstall),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying installation failed")
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout, clusterErrorInfo)
		})

		It("install_cluster assisted-installer already running", func() {
			By("Installing cluster")
			c := installCluster(clusterID)
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalling))
			Expect(swag.StringValue(c.StatusInfo)).Should(Equal("Installation in progress"))
			Expect(len(c.Hosts)).Should(Equal(5))
			for _, host := range c.Hosts {
				Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusInstalling))
			}

			// post failure to execute the install command due to a running assisted-installer
			_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
				InfraEnvID: *infraEnvID,
				HostID:     *c.Hosts[0].ID,
				Reply: &models.StepReply{
					ExitCode: bminventory.ContainerAlreadyRunningExitCode,
					StepType: models.StepTypeInstall,
					Output:   "blabla",
					Error:    "Trying to pull registry.stage.redhat.io/openshift4/assisted-installer-rhel8:v4.6.0-19...\nGetting image source signatures\nCopying blob sha256:e5fbed36397a9434b3330d01bcf53befb828e476be291c8e1c026a9753d59dfd\nCopying blob sha256:0fd3b5213a9b4639d32bf2ef6a3d7cc9891c4d8b23639ff7ae99d66ecb490a70\nCopying blob sha256:aebb8c5568533b57ee3da86262f7bff81383a2a624b9f54b9da3418705009901\nCopying config sha256:67bbdf0b8fb27217b9c5f6fa3593925309ef8c95b8b0be8b44713ba5f826fcee\nWriting manifest to image destination\nStoring signatures\nError: error creating container storage: the container name \"assisted-installer\" is already in use by \"331fe687f4af5c7adf75a9ddaaadfb801739ba1815bfcaafd4db7392bf9049bc\". You have to remove that container to be able to reuse that name.: that name is already in use\n",
					StepID:   string(models.StepTypeInstall) + "-123465",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			By("Verify host status is still installing")
			_, status, _ := isHostInState(ctx, *infraEnvID, *c.Hosts[0].ID, models.HostStatusInstalling)
			Expect(status).Should(Equal(models.HostStatusInstalling))

		})

		Context("report logs progress", func() {
			verifyLogProgress := func(c *models.Cluster, host_progress models.LogsState, cluster_progress models.LogsState) {
				Expect(c.ControllerLogsStartedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
				Expect(c.LogsInfo).To(Equal(cluster_progress))
				for _, host := range c.Hosts {
					Expect(host.LogsStartedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
					Expect(host.LogsInfo).To(Equal(host_progress))
				}
			}
			It("reset log fields before installation", func() {
				By("set log fields to a non-zero value")
				cluster = getCluster(clusterID)
				db.Model(cluster).Updates(map[string]interface{}{
					"logs_info":                    "requested",
					"controller_logs_started_at":   strfmt.DateTime(time.Now()),
					"controller_logs_collected_at": strfmt.DateTime(time.Now()),
				})
				for _, host := range cluster.Hosts {
					db.Model(&common.Host{}).Where("id = ?", host.ID.String()).Updates(map[string]interface{}{
						"logs_info":         "requested",
						"logs_started_at":   strfmt.DateTime(time.Now()),
						"logs_collected_at": strfmt.DateTime(time.Now()),
					})
				}

				By("start installation")
				c := installCluster(clusterID)

				By("verify timestamps")
				//This can work only because in subsystem there is no actual log upload
				//from the hosts. What we are verifying is that the db fields were cleared
				//during the installation initiation. We can not stop the test reliably at
				//the preperation successful stage because it is very brief
				Expect(string(c.LogsInfo)).To(BeEmpty())
				Expect(c.ControllerLogsStartedAt).To(Equal(strfmt.DateTime(time.Time{})))
				Expect(c.ControllerLogsCollectedAt).To(Equal(strfmt.DateTime(time.Time{})))
				for _, host := range cluster.Hosts {
					Expect(host.LogsStartedAt).To(Equal(strfmt.DateTime(time.Time{})))
					Expect(host.LogsCollectedAt).To(Equal(strfmt.DateTime(time.Time{})))
					Expect(string(host.LogsInfo)).To(BeEmpty())
				}
			})

			It("log progress installation succeed", func() {
				By("report log progress by host and cluster during installation")
				c := installCluster(clusterID)
				requested := models.LogsStateRequested
				completed := models.LogsStateCompleted
				for _, host := range c.Hosts {
					updateHostLogProgress(host.InfraEnvID, *host.ID, requested)
				}
				updateClusterLogProgress(clusterID, requested)

				c = getCluster(clusterID)
				verifyLogProgress(c, requested, requested)

				By("report log progress by cluster during finalizing")
				for _, host := range c.Hosts {
					updateHostLogProgress(host.InfraEnvID, *host.ID, completed)
					updateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
				}
				waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
				updateClusterLogProgress(clusterID, requested)
				c = getCluster(clusterID)
				verifyLogProgress(c, completed, requested)

				By("report log progress by cluster after installation")
				completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
				updateClusterLogProgress(clusterID, completed)
				c = getCluster(clusterID)
				verifyLogProgress(c, completed, completed)
			})
		})

		It("report_host_progress", func() {
			c := installCluster(clusterID)
			hosts := getClusterMasters(c)

			By("invalid_report", func() {
				step := models.HostStage("INVALID REPORT")
				installProgress := &models.HostProgress{
					CurrentStage: step,
				}

				_, err := agentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   hosts[0].InfraEnvID,
					HostProgress: installProgress,
					HostID:       *hosts[0].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			// Host #1

			By("progress_to_other_host", func() {
				installProgress := models.HostStageWritingImageToDisk
				installInfo := "68%"
				updateHostProgressWithInfo(*hosts[0].ID, *infraEnvID, installProgress, installInfo)
				hostFromDB := getHostV2(*infraEnvID, *hosts[0].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(installInfo))
			})

			By("report_done", func() {
				installProgress := models.HostStageDone
				updateProgress(*hosts[0].ID, *infraEnvID, installProgress)
				hostFromDB := getHostV2(*infraEnvID, *hosts[0].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstalled))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("cant_report_after_done", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageFailed,
				}

				_, err := agentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   hosts[0].InfraEnvID,
					HostProgress: installProgress,
					HostID:       *hosts[0].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			// Host #2

			By("progress_to_some_host", func() {
				installProgress := models.HostStageWritingImageToDisk
				updateProgress(*hosts[1].ID, hosts[1].InfraEnvID, installProgress)
				hostFromDB := getHostV2(*infraEnvID, *hosts[1].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("invalid_lower_stage", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageInstalling,
				}

				_, err := agentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   hosts[1].InfraEnvID,
					HostProgress: installProgress,
					HostID:       *hosts[1].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			By("report_failed_on_same_host", func() {
				installProgress := models.HostStageFailed
				installInfo := "because some error"
				updateHostProgressWithInfo(*hosts[1].ID, *infraEnvID, installProgress, installInfo)
				hostFromDB := getHostV2(*infraEnvID, *hosts[1].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", installProgress, installInfo)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk)) // Last stage
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("cant_report_after_error", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageDone,
				}

				_, err := agentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   hosts[1].InfraEnvID,
					HostProgress: installProgress,
					HostID:       *hosts[1].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			By("verify_everything_changed_error", func() {
				waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
					IgnoreStateInfo)
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				waitForHostState(ctx, models.HostStatusError, defaultWaitForHostStateTimeout, c.Hosts...)
			})
		})

		It("[minimal-set]install download_config_files", func() {
			//Test downloading kubeconfig files in worng state
			//This test uses Agent Auth for DownloadClusterFiles (as opposed to the other tests), to cover both supported authentication types for this API endpoint.
			file, err := os.CreateTemp("", "tmp")
			Expect(err).NotTo(HaveOccurred())

			defer os.Remove(file.Name())
			_, err = agentBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2DownloadClusterFilesConflict()))

			installCluster(clusterID)

			missingClusterId := strfmt.UUID(uuid.New().String())
			_, err = agentBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: missingClusterId, FileName: "bootstrap.ign"}, file)
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2DownloadClusterFilesNotFound()))

			_, err = agentBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: clusterID, FileName: "not_real_file"}, file)
			Expect(err).Should(HaveOccurred())

			_, err = agentBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("download_config_files in error state", func() {
			file, err := os.CreateTemp("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(file.Name())

			FailCluster(ctx, clusterID, *infraEnvID, masterFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)

			_, err = userBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))

			By("Download install-config.yaml")
			_, err = userBMClient.Installer.V2DownloadClusterFiles(ctx, &installer.V2DownloadClusterFilesParams{ClusterID: clusterID, FileName: "install-config.yaml"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err = file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("Get credentials", func() {
			By("Test getting credentials for not found cluster")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: missingClusterId})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsNotFound()))
			}
			By("Test getting credentials before console operator is available")
			{
				_, err := userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsConflict()))
			}
			By("Test happy flow")
			{
				setClusterAsFinalizing(ctx, clusterID)
				completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
				creds, err := userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(creds.GetPayload().Username).To(Equal(bminventory.DefaultUser))
				Expect(creds.GetPayload().ConsoleURL).To(Equal(common.GetConsoleUrl(cluster.Name, cluster.BaseDNSDomain)))
				Expect(len(creds.GetPayload().Password)).NotTo(Equal(0))
			}
		})

		It("Transform installed cluster to day2", func() {
			setClusterAsFinalizing(ctx, clusterID)
			completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
			clusterDay2, err := userBMClient.Installer.TransformClusterToDay2(ctx, &installer.TransformClusterToDay2Params{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(clusterDay2.GetPayload().Status)).Should(Equal(models.ClusterStatusAddingHosts))
			Expect(swag.StringValue(clusterDay2.GetPayload().Kind)).Should(Equal(models.ClusterKindAddHostsCluster))
		})

		It("Upload and Download logs", func() {
			By("Download before upload")
			{
				nodes, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
				file, err := os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{ClusterID: clusterID, HostID: nodes[1].ID}, file)
				Expect(err).NotTo(HaveOccurred())

			}

			By("Test happy flow small file")
			{
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				_, _ = register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
				_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: clusterID, LogsType: string(models.LogsTypeController), Upfile: kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())
				logsType := string(models.LogsTypeController)
				file, err := os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{ClusterID: clusterID, LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
			}

			By("Test happy flow node logs file")
			{
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				logsType := string(models.LogsTypeHost)
				hosts, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
				_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{
					ClusterID:  clusterID,
					HostID:     hosts[0].ID,
					InfraEnvID: infraEnvID,
					LogsType:   logsType,
					Upfile:     kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())

				file, err := os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{
					ClusterID: clusterID,
					HostID:    hosts[0].ID,
					LogsType:  &logsType,
				}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
			}

			By("Test happy flow large file")
			{
				filePath := "../build/test_logs.txt"
				// open the out file for writing
				outfile, err := os.Create(filePath)
				Expect(err).NotTo(HaveOccurred())
				defer outfile.Close()
				cmd := exec.Command("head", "-c", "200MB", "/dev/urandom")
				err = cmd.Run()
				Expect(err).NotTo(HaveOccurred())
				nodes, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
				// test hosts logs
				kubeconfigFile, err := os.Open(filePath)
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: clusterID, HostID: nodes[1].ID,
					InfraEnvID: &nodes[1].InfraEnvID, Upfile: kubeconfigFile, LogsType: string(models.LogsTypeHost)})
				Expect(err).NotTo(HaveOccurred())
				h := getHostV2(*infraEnvID, *nodes[1].ID)
				Expect(h.LogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
				logsType := string(models.LogsTypeHost)
				file, err := os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{ClusterID: clusterID,
					HostID: nodes[1].ID, LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
				// test controller logs
				kubeconfigFile, err = os.Open(filePath)
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: clusterID,
					Upfile: kubeconfigFile, LogsType: string(models.LogsTypeController)})
				Expect(err).NotTo(HaveOccurred())
				c := getCluster(clusterID)
				Expect(c.ControllerLogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
				logsType = string(models.LogsTypeController)
				file, err = os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{ClusterID: clusterID,
					LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err = file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
			}
		})

		uploadManifest := func(content string, folder string, filename string) {
			base64Content := base64.StdEncoding.EncodeToString([]byte(content))
			response, err := userBMClient.Manifests.V2CreateClusterManifest(ctx, &manifests.V2CreateClusterManifestParams{
				ClusterID: clusterID,
				CreateManifestParams: &models.CreateManifestParams{
					Content:  &base64Content,
					FileName: &filename,
					Folder:   &folder,
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*response.Payload).Should(Not(BeNil()))
		}

		manifest1Content := `apiVersion: v1
kind: Namespace
metadata:
name: exampleNamespace1`

		manifest2Content := `apiVersion: v1
kind: Namespace
metadata:
name: exampleNamespace2`

		It("Download cluster logs", func() {
			// Add some manifest files and then verify that these are added to the log...
			nodes, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
			for _, host := range nodes {
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: clusterID,
					InfraEnvID: &host.InfraEnvID, HostID: host.ID, LogsType: string(models.LogsTypeHost), Upfile: kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())
				kubeconfigFile.Close()
			}
			kubeconfigFile, err := os.Open("test_kubeconfig")
			Expect(err).NotTo(HaveOccurred())
			_, err = agentBMClient.Installer.V2UploadLogs(ctx, &installer.V2UploadLogsParams{ClusterID: clusterID,
				LogsType: string(models.LogsTypeController), Upfile: kubeconfigFile})
			Expect(err).NotTo(HaveOccurred())
			kubeconfigFile.Close()

			uploadManifest(manifest1Content, "openshift", "manifest1.yaml")
			uploadManifest(manifest2Content, "openshift", "manifest2.yaml")

			filePath := "../build/test_logs.tar"
			file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			Expect(err).NotTo(HaveOccurred())
			defer file.Close()
			logsType := string(models.LogsTypeAll)
			_, err = userBMClient.Installer.V2DownloadClusterLogs(ctx, &installer.V2DownloadClusterLogsParams{ClusterID: clusterID, LogsType: &logsType}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
			file.Close()
			file, err = os.Open(filePath)
			Expect(err).NotTo(HaveOccurred())
			tarReader := tar.NewReader(file)
			numOfarchivedFiles := 0
			expectedFiles := []string{"cluster_manifest_user-supplied_openshift_manifest1.yaml", "cluster_manifest_user-supplied_openshift_manifest2.yaml", "cluster_events.json", "cluster_metadata.json", "controller_logs.tar.gz", "test-cluster_auto-assign_h1.tar", "test-cluster_auto-assign_h2.tar", "test-cluster_auto-assign_h3.tar"}
			for {
				header, err := tarReader.Next()
				if err == io.EOF {
					break
				}
				Expect(swag.ContainsStrings(expectedFiles, header.Name)).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
				numOfarchivedFiles += 1
			}
			Expect(numOfarchivedFiles).Should(Equal(len(expectedFiles)))
		})

		It("Upload ingress ca and kubeconfig download", func() {

			By("Upload ingress ca for not existent clusterid")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: missingClusterId, IngressCertParams: "dummy"})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertNotFound()))
			}
			By("Test getting upload ingress ca in wrong state")
			{
				_, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: "dummy"})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertBadRequest()))
			}
			By("Test happy flow")
			{
				setClusterAsFinalizing(ctx, clusterID)
				// Download kubeconfig before uploading
				kubeconfigNoIngress, err := os.CreateTemp("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2DownloadClusterCredentials(ctx, &installer.V2DownloadClusterCredentialsParams{ClusterID: clusterID, FileName: "kubeconfig-noingress"}, kubeconfigNoIngress)
				Expect(err).ToNot(HaveOccurred())
				sni, err := kubeconfigNoIngress.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(sni.Size()).ShouldNot(Equal(0))

				By("Trying to download kubeconfig file before it exists")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
				Expect(err).Should(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsConflict()))

				By("Upload ingress ca")
				res, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertCreated()))

				// Download kubeconfig after uploading
				completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(err).NotTo(HaveOccurred())
			}
			By("Try to upload ingress ca second time, do nothing and return ok")
			{
				// Try to upload ingress ca second time
				res, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertCreated()))
			}
		})

		It("on cluster error - verify all hosts are aborted", func() {
			FailCluster(ctx, clusterID, *infraEnvID, masterFailure)
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout, clusterErrorInfo)
			rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			waitForHostState(ctx, models.HostStatusError, defaultWaitForHostStateTimeout, c.Hosts...)
		})

		Context("cancel installation", func() {
			It("cancel running installation", func() {
				c := installCluster(clusterID)
				waitForHostState(ctx, models.HostStatusInstalling, defaultWaitForHostStateTimeout, c.Hosts...)
				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForClusterState(ctx, clusterID, models.ClusterStatusCancelled, defaultWaitForClusterStateTimeout, clusterCanceledInfo)
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
				for _, host := range c.Hosts {
					Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusCancelled))
				}

				Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
			})
			It("cancel installation conflicts", func() {
				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2CancelInstallationConflict()))
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusReady))
			})
			It("cancel failed cluster", func() {
				By("verify cluster is in error")
				FailCluster(ctx, clusterID, *infraEnvID, masterFailure)
				waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
					clusterErrorInfo)
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusError))
				waitForHostState(ctx, models.HostStatusError, defaultWaitForHostStateTimeout, c.Hosts...)
				By("cancel installation, check cluster and hosts statuses")
				_, err = userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				rep, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				c = rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
				for _, host := range c.Hosts {
					Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusCancelled))
				}
			})
			It("cancel cluster with various hosts states", func() {
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(5))

				updateProgress(*c.Hosts[0].ID, c.Hosts[0].InfraEnvID, "Installing")
				updateProgress(*c.Hosts[1].ID, c.Hosts[1].InfraEnvID, "Done")

				h1 := getHostV2(*infraEnvID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHostV2(*infraEnvID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				waitForHostState(ctx, models.HostStatusCancelled, defaultWaitForClusterStateTimeout, c.Hosts...)
			})

			It("cancel host - wrong boot order", func() {
				c := installCluster(clusterID)
				hostID := c.Hosts[0].ID
				Expect(isStepTypeInList(getNextSteps(*infraEnvID, *hostID), models.StepTypeInstall)).Should(BeTrue())
				updateProgress(*hostID, *infraEnvID, models.HostStageRebooting)

				_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
					InfraEnvID: *infraEnvID,
					NewHostParams: &models.HostCreateParams{
						HostID: hostID,
					},
				})
				Expect(err).ShouldNot(HaveOccurred())
				hostInDb := getHostV2(*infraEnvID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingPendingUserAction))

				waitForClusterState(
					ctx,
					clusterID,
					models.ClusterStatusInstallingPendingUserAction,
					defaultWaitForClusterStateTimeout,
					clusterInstallingPendingUserActionStateInfo)

				_, err = userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				waitForHostState(ctx, models.HostStatusCancelled, defaultWaitForHostStateTimeout, c.Hosts...)
			})
			It("cancel installation - cluster in finalizing status", func() {
				setClusterAsFinalizing(ctx, clusterID)
				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())

				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(c).NotTo(BeNil())

				waitForHostState(ctx, models.HostStatusCancelled, defaultWaitForHostStateTimeout, c.Hosts...)
			})
		})
		Context("reset installation", func() {
			enableReset, _ := strconv.ParseBool(os.Getenv("ENABLE_RESET"))

			verifyHostProgressReset := func(progress *models.HostProgressInfo) {
				Expect(progress).NotTo(BeNil())
				Expect(string(progress.CurrentStage)).To(Equal(""))
				Expect(progress.InstallationPercentage).To(Equal(int64(0)))
				Expect(progress.ProgressInfo).To(Equal(""))
				Expect(progress.StageStartedAt).To(Equal(strfmt.DateTime(time.Time{})))
				Expect(progress.StageUpdatedAt).To(Equal(strfmt.DateTime(time.Time{})))
			}

			It("reset cluster and register hosts", func() {
				By("verify reset success")
				installCluster(clusterID)
				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())

				By("verify cluster state")
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))

				By("verify resetted fields")
				expectProgressToBe(c, 0, 0, 0)
				Expect(c.OpenshiftClusterID.String()).To(Equal(""))

				By("verify hosts state and resetted fields")
				ips := hostutil.GenerateIPv4Addresses(len(c.Hosts), defaultCIDRv4)
				for i, host := range c.Hosts {
					if enableReset {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
						steps := getNextSteps(*infraEnvID, *host.ID)
						Expect(len(steps.Instructions)).Should(Equal(0))
					} else {
						waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForHostStateTimeout, host)
					}
					verifyHostProgressReset(host.Progress)

					_, err = agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
						InfraEnvID: *infraEnvID,
						NewHostParams: &models.HostCreateParams{
							HostID: host.ID,
						},
					})
					Expect(err).ShouldNot(HaveOccurred())
					waitForHostState(ctx, models.HostStatusDiscovering, defaultWaitForHostStateTimeout, host)
					generateEssentialHostSteps(ctx, host, fmt.Sprintf("host-after-reset-%d", i), ips[i])
				}
				generateFullMeshConnectivity(ctx, ips[0], c.Hosts...)
				for _, host := range c.Hosts {
					waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, host)
					host = getHostV2(*infraEnvID, *host.ID)
					Expect(host.Progress.CurrentStage).Should(Equal(models.HostStage("")))
					Expect(host.Progress.ProgressInfo).Should(Equal(""))
					Expect(host.Bootstrap).Should(Equal(false))
				}
			})
			It("reset cluster and remove bootstrap", func() {
				if enableReset {
					var bootstrapID *strfmt.UUID

					By("verify reset success")
					installCluster(clusterID)
					_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					_, err = userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					c := rep.GetPayload()
					for _, h := range c.Hosts {
						if h.Bootstrap {
							bootstrapID = h.ID
							break
						}
					}
					Expect(bootstrapID).ShouldNot(Equal(nil))

					By("verify cluster state")
					rep, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					c = rep.GetPayload()
					Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))

					By("register hosts and disable bootstrap")
					ips := hostutil.GenerateIPv4Addresses(len(c.Hosts), defaultCIDRv4)
					for i, host := range c.Hosts {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
						steps := getNextSteps(*infraEnvID, *host.ID)
						Expect(len(steps.Instructions)).Should(Equal(0))
						_, err = agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
							InfraEnvID: *infraEnvID,
							NewHostParams: &models.HostCreateParams{
								HostID: host.ID,
							},
						})
						Expect(err).ShouldNot(HaveOccurred())
						waitForHostState(ctx, models.HostStatusDiscovering, defaultWaitForHostStateTimeout, host)
						generateEssentialHostSteps(ctx, host, fmt.Sprintf("host-after-reset-%d", i), ips[i])
					}
					generateFullMeshConnectivity(ctx, ips[0], c.Hosts...)
					for _, host := range c.Hosts {
						waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, host)

						if host.Bootstrap {
							_, err = userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
								InfraEnvID: host.InfraEnvID,
								HostID:     *host.ID,
							})
							Expect(err).NotTo(HaveOccurred())
						}
					}
					h := registerNode(ctx, *infraEnvID, "hostname", defaultCIDRv4)
					_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
						HostUpdateParams: &models.HostUpdateParams{
							HostRole: swag.String(string(models.HostRoleMaster)),
						},
						HostID:     *h.ID,
						InfraEnvID: *infraEnvID,
					})
					Expect(err).NotTo(HaveOccurred())

					By("check for a new bootstrap")
					waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
						clusterReadyStateInfo)
					c = installCluster(clusterID)
					for _, h := range c.Hosts {
						if h.Bootstrap {
							Expect(h.ID).ShouldNot(Equal(bootstrapID))
							break
						}
					}
				}
			})
			It("reset ready/installing cluster", func() {
				_, err := userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).To(BeAssignableToTypeOf(installer.NewV2ResetClusterConflict()))
				c := installCluster(clusterID)
				waitForHostState(ctx, models.HostStatusInstalling, defaultWaitForHostStateTimeout, c.Hosts...)
				_, err = userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				for _, host := range c.Hosts {
					if enableReset {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
					} else {
						waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForHostStateTimeout, host)
					}
				}
			})
			It("reset cluster with various hosts states", func() {
				c := installCluster(clusterID)
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalling))
				Expect(len(c.Hosts)).Should(Equal(5))

				updateProgress(*c.Hosts[0].ID, c.Hosts[0].InfraEnvID, "Installing")
				updateProgress(*c.Hosts[1].ID, c.Hosts[1].InfraEnvID, "Done")

				h1 := getHostV2(*infraEnvID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHostV2(*infraEnvID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err := userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForClusterStateTimeout, c.Hosts...)
			})

			It("reset cluster - wrong boot order", func() {
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(5))
				updateProgress(*c.Hosts[0].ID, c.Hosts[0].InfraEnvID, models.HostStageRebooting)
				_, err := userBMClient.Installer.V2CancelInstallation(ctx, &installer.V2CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterResetStateInfo)
				for _, host := range c.Hosts {
					waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForHostStateTimeout, host)
					_, err = agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
						InfraEnvID: *infraEnvID,
						NewHostParams: &models.HostCreateParams{
							HostID: host.ID,
						},
					})
					Expect(err).ShouldNot(HaveOccurred())
					waitForHostState(ctx, models.HostStatusDiscovering, defaultWaitForHostStateTimeout, host)
				}
			})

			It("reset installation - cluster in finalizing status", func() {
				setClusterAsFinalizing(ctx, clusterID)
				_, err := userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())

				rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(c).NotTo(BeNil())

				waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForHostStateTimeout, c.Hosts...)
			})

			It("reset cluster doesn't delete user generated manifests", func() {
				content := `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 01-user-generated-manifest
spec:
  kernelArguments:
  - 'loglevel=7'`
				base64Content := base64.StdEncoding.EncodeToString([]byte(content))
				manifest := models.Manifest{
					FileName: "01-user-generated-manifest.yaml",
					Folder:   "openshift",
				}
				// All manifests created via the API are considered to be "user generated"
				response, err := userBMClient.Manifests.V2CreateClusterManifest(ctx, &manifests.V2CreateClusterManifestParams{
					ClusterID: clusterID,
					CreateManifestParams: &models.CreateManifestParams{
						Content:  &base64Content,
						FileName: &manifest.FileName,
						Folder:   &manifest.Folder,
					},
				})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*response.Payload).Should(Equal(manifest))

				c := installCluster(clusterID)
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalling))
				Expect(len(c.Hosts)).Should(Equal(5))

				updateProgress(*c.Hosts[0].ID, c.Hosts[0].InfraEnvID, "Installing")
				updateProgress(*c.Hosts[1].ID, c.Hosts[1].InfraEnvID, "Done")

				h1 := getHostV2(*infraEnvID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHostV2(*infraEnvID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err = userBMClient.Installer.V2ResetCluster(ctx, &installer.V2ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForHostState(ctx, models.HostStatusResettingPendingUserAction, defaultWaitForClusterStateTimeout, c.Hosts...)

				// verify manifest remains after cluster reset
				response2, err := userBMClient.Manifests.V2ListClusterManifests(ctx, &manifests.V2ListClusterManifestsParams{
					ClusterID: *cluster.ID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(response2.Payload).Should(ContainElement(&manifest))
			})

			AfterEach(func() {
				reply, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(reply.GetPayload().OpenshiftClusterID).To(Equal(*strToUUID("")))
			})
		})
	})

	Context("NoProxy with Wildcard", func() {

		It("OpenshiftVersion does support NoProxy wildcard", func() {
			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:        "example.com",
					ClusterNetworks:      []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:      []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:                 swag.String("sno-cluster"),
					OpenshiftVersion:     &openshiftVersion,
					NoProxy:              swag.String("*"),
					PullSecret:           swag.String(pullSecret),
					SSHPublicKey:         sshPublicKey,
					VipDhcpAllocation:    swag.Bool(false),
					NetworkType:          swag.String("OVNKubernetes"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("install cluster requirement", func() {
		clusterID := *cluster.ID
		waitForClusterState(ctx, clusterID, models.ClusterStatusPendingForInput, defaultWaitForClusterStateTimeout,
			clusterPendingForInputStateInfo)

		checkUpdateAtWhileStatic(ctx, clusterID)

		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		newIPs := hostutil.GenerateIPv4Addresses(2, ips[2])
		h4 := &registerHost(*infraEnvID).Host
		h5 := registerNode(ctx, *infraEnvID, "h5", newIPs[0])

		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"

		By("Two hosts are masters, one host is without role  -> state must be insufficient")
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[1].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				APIVips:     []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
				IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		// add host and 2 workers (h4 has no inventory) --> insufficient state due to single worker
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[2].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h5.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		// update host4 again (now it has inventory) -> state must be ready
		generateEssentialHostSteps(ctx, h4, "h4", newIPs[1])
		// update role for the host4 to master -> state must be ready
		generateFullMeshConnectivity(ctx, ips[0], hosts[0], hosts[1], hosts[2], h4, h5)
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)
	})

	It("install_cluster_states", func() {
		clusterID := *cluster.ID
		waitForClusterState(ctx, clusterID, models.ClusterStatusPendingForInput, 60*time.Second, clusterPendingForInputStateInfo)
		ips := hostutil.GenerateIPv4Addresses(6, defaultCIDRv4)
		wh1 := registerNode(ctx, *infraEnvID, "wh1", ips[0])
		wh2 := registerNode(ctx, *infraEnvID, "wh2", ips[1])
		wh3 := registerNode(ctx, *infraEnvID, "wh3", ips[2])
		generateFullMeshConnectivity(ctx, ips[0], wh1, wh2, wh3)

		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"

		By("All hosts are workers -> state must be insufficient")
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *wh1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *wh2.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *wh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
				IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)
		clusterReply, getErr := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
			ClusterID: clusterID,
		})
		Expect(getErr).ToNot(HaveOccurred())
		Expect(string(clusterReply.Payload.APIVips[0].IP)).To(Equal(apiVip))
		Expect(string(clusterReply.Payload.IngressVips[0].IP)).To(Equal(ingressVip))
		Expect(string(clusterReply.Payload.MachineNetworks[0].Cidr)).To(Equal("1.2.3.0/24"))
		Expect(len(clusterReply.Payload.HostNetworks)).To(Equal(1))
		Expect(clusterReply.Payload.HostNetworks[0].Cidr).To(Equal("1.2.3.0/24"))

		mh1 := registerNode(ctx, *infraEnvID, "mh1", ips[3])
		generateFAPostStepReply(ctx, mh1, validFreeAddresses)
		mh2 := registerNode(ctx, *infraEnvID, "mh2", ips[4])
		mh3 := registerNode(ctx, *infraEnvID, "mh3", ips[5])
		generateFullMeshConnectivity(ctx, ips[0], mh1, mh2, mh3, wh1, wh2, wh3)
		clusterReply, _ = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
			ClusterID: clusterID,
		})

		hids := make([]interface{}, 0)
		for _, h := range clusterReply.Payload.HostNetworks[0].HostIds {
			hids = append(hids, h)
		}
		Expect(len(hids)).To(Equal(6))
		Expect(*wh1.ID).To(BeElementOf(hids...))
		Expect(*wh2.ID).To(BeElementOf(hids...))
		Expect(*wh3.ID).To(BeElementOf(hids...))
		Expect(*mh1.ID).To(BeElementOf(hids...))
		Expect(*mh2.ID).To(BeElementOf(hids...))
		Expect(*mh3.ID).To(BeElementOf(hids...))

		By("Only two masters -> state must be insufficient")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *mh1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *mh2.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *mh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		By("Three master hosts -> state must be ready")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *mh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, mh3)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout, clusterReadyStateInfo)

		By("Back to two master hosts -> state must be insufficient")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *mh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		cluster = getCluster(clusterID)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		Expect(swag.StringValue(cluster.StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		By("Three master hosts -> state must be ready")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *mh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, mh3)
		waitForClusterState(ctx, clusterID, "ready", 60*time.Second, clusterReadyStateInfo)

		By("Back to two master hosts -> state must be insufficient")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *mh3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		cluster = getCluster(clusterID)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		Expect(swag.StringValue(cluster.StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		_, err = userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetClusterNotFound()))
	})

	It("install_cluster_insufficient_master", func() {
		clusterID := *cluster.ID

		By("set host with log hw info for master")
		hwInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 2},
			Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB), UsableBytes: int64(8 * units.GiB)},
			Disks:  []*models.Disk{&sdb},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
			SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
			Routes:       common.TestDefaultRouteConfiguration,
		}
		h1 := &registerHost(*infraEnvID).Host
		generateEssentialHostStepsWithInventory(ctx, h1, "h1", hwInfo)
		apiVip := "1.2.3.8"
		ingressVip := "1.2.3.9"
		_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
				IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Register 3 more hosts with valid hw info")
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		h2 := registerNode(ctx, *infraEnvID, "h2", ips[0])
		h3 := registerNode(ctx, *infraEnvID, "h3", ips[1])
		h4 := registerNode(ctx, *infraEnvID, "h4", ips[2])

		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4)
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h1)

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h2.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("validate that host 1 is insufficient")
		waitForHostState(ctx, models.HostStatusInsufficient, defaultWaitForClusterStateTimeout, h1)
	})

	It("install_cluster with edge worker", func() {
		clusterID := *cluster.ID

		By("set host with log hw info for master")
		sdc := models.Disk{
			ID:        sdbId,
			ByID:      sdbId,
			DriveType: "HDD",
			Name:      "sdc",
			SizeBytes: int64(17179869184),
		}

		hwInfo := &models.Inventory{
			CPU:    &models.CPU{Count: 2, Architecture: common.AARCH64CPUArchitecture},
			Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB), UsableBytes: int64(8 * units.GiB)},
			Disks:  []*models.Disk{&sdc},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
			SystemVendor: &models.SystemVendor{Manufacturer: "mellanox", ProductName: "Bluefield soc", SerialNumber: "3534"},
			Routes:       common.TestDefaultRouteConfiguration,
		}

		By("Register edge worker with 16Gb disk")
		h1 := &registerHost(*infraEnvID).Host
		generateEssentialHostStepsWithInventory(ctx, h1, "h1", hwInfo)

		By("Register rergular worker with 16Gb disk")
		hwInfo.SystemVendor.ProductName = "ding dong soc"
		h5 := &registerHost(*infraEnvID).Host
		generateEssentialHostStepsWithInventory(ctx, h5, "h5", hwInfo)

		apiVip := "1.2.3.8"
		ingressVip := "1.2.3.9"
		_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
				IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Register 3 more hosts with valid hw info")
		ips := hostutil.GenerateIPv4Addresses(3, defaultCIDRv4)
		h2 := registerNode(ctx, *infraEnvID, "h2", ips[0])
		h3 := registerNode(ctx, *infraEnvID, "h3", ips[1])
		h4 := registerNode(ctx, *infraEnvID, "h4", ips[2])

		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5)

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h5.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h2.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("validate that host 5 that is not edge worker is insufficient")
		waitForHostState(ctx, models.HostStatusInsufficient, defaultWaitForClusterStateTimeout, h5)
		h5 = getHostV2(*infraEnvID, *h5.ID)
		Expect(h5.ValidationsInfo).Should(ContainSubstring("No eligible disks were found"))

		By("validate that edge worker is passing the validation")
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForClusterStateTimeout, h1)
	})

	It("unique_hostname_validation", func() {
		clusterID := *cluster.ID
		//define h1 as known master
		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHostV2(*infraEnvID, *hosts[0].ID)
		h2 := getHostV2(*infraEnvID, *hosts[1].ID)
		h3 := getHostV2(*infraEnvID, *hosts[2].ID)
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)
		waitForHostState(ctx, "known", 60*time.Second, h1)
		Expect(h1.RequestedHostname).Should(Equal("h1"))

		By("Registering host with same hostname")
		newIPs := hostutil.GenerateIPv4Addresses(2, ips[2])
		//after name clash --> h1 and h4 are insufficient
		h4 := registerNode(ctx, *infraEnvID, "h1", newIPs[0])
		h4 = getHostV2(*infraEnvID, *h4.ID)
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4)
		waitForHostState(ctx, "insufficient", 60*time.Second, h1)
		Expect(h4.RequestedHostname).Should(Equal("h1"))
		h1 = getHostV2(*infraEnvID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))

		By("Verifying install command")
		//install cluster should fail because only 2 hosts are known
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *h1.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[1].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[2].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())

		By("Registering one more host with same hostname")
		disabledHost := registerNode(ctx, *infraEnvID, "h1", newIPs[1])
		disabledHost = getHostV2(*infraEnvID, *disabledHost.ID)
		waitForHostState(ctx, models.HostStatusInsufficient, defaultWaitForHostStateTimeout, disabledHost)
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *disabledHost.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Changing hostname, verify host is known now")
		generateEssentialHostSteps(ctx, h4, "h4", newIPs[0])
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h4)
		h4 = getHostV2(*infraEnvID, *h4.ID)
		Expect(h4.RequestedHostname).Should(Equal("h4"))

		By("Remove host with the same hostname and verify h1 is known")
		_, err = userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: disabledHost.InfraEnvID,
			HostID:     *disabledHost.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, h1)

		By("add one more worker to get 2 functioning workers")
		h5 := registerNode(ctx, *infraEnvID, "h5", newIPs[1])
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h5.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5)

		By("waiting for cluster to be in ready state")
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

		By("Verify install after disabling the host with same hostname")
		_, err = userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})

	It("localhost is not valid", func() {
		localhost := "localhost"
		clusterID := *cluster.ID

		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHostV2(*infraEnvID, *hosts[0].ID)
		waitForHostState(ctx, "known", 60*time.Second, h1)
		Expect(h1.RequestedHostname).Should(Equal("h1"))

		By("Changing hostname reply to localhost")
		generateEssentialHostSteps(ctx, h1, localhost, ips[0])
		waitForHostState(ctx, models.HostStatusInsufficient, 60*time.Second, h1)
		h1Host := getHostV2(*infraEnvID, *h1.ID)
		Expect(h1Host.RequestedHostname).Should(Equal(localhost))

		By("Setting hostname to valid name")
		hostname := "reqh0"
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &hostname,
			},
			HostID:     *h1Host.ID,
			InfraEnvID: h1Host.InfraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, models.HostStatusKnown, 60*time.Second, h1)

		By("Setting hostname to localhost should cause an API error")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &localhost,
			},
			HostID:     *h1Host.ID,
			InfraEnvID: h1Host.InfraEnvID,
		})
		Expect(err).To(HaveOccurred())

		waitForHostState(ctx, models.HostStatusKnown, 60*time.Second, h1)

	})

	It("different_roles_stages", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
		c := installCluster(clusterID)
		Expect(len(c.Hosts)).Should(Equal(5))

		var atLeastOneBootstrap bool = false

		for _, h := range c.Hosts {
			if h.Bootstrap {
				Expect(h.ProgressStages).Should(Equal(host.BootstrapStages[:]))
				atLeastOneBootstrap = true
			} else if h.Role == models.HostRoleMaster {
				Expect(h.ProgressStages).Should(Equal(host.MasterStages[:]))
			} else {
				Expect(h.ProgressStages).Should(Equal(host.WorkerStages[:]))
			}
		}

		Expect(atLeastOneBootstrap).Should(BeTrue())
	})

	It("set_requested_hostnames", func() {
		clusterID := *cluster.ID
		hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[1].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleMaster)),
			},
			HostID:     *hosts[2].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHostV2(*infraEnvID, *hosts[0].ID)
		h2 := getHostV2(*infraEnvID, *hosts[1].ID)
		h3 := getHostV2(*infraEnvID, *hosts[2].ID)
		waitForHostState(ctx, models.HostStatusKnown, time.Minute, h1, h2, h3)
		// update requested hostnames
		hostname := "reqh0"
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &hostname,
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: hosts[0].InfraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		hostname = "reqh1"
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &hostname,
			},
			HostID:     *hosts[1].ID,
			InfraEnvID: hosts[1].InfraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		// check hostnames were updated
		h1 = getHostV2(*infraEnvID, *h1.ID)
		h2 = getHostV2(*infraEnvID, *h2.ID)
		h3 = getHostV2(*infraEnvID, *h3.ID)
		Expect(h1.RequestedHostname).Should(Equal("reqh0"))
		Expect(h2.RequestedHostname).Should(Equal("reqh1"))
		Expect(*h1.Status).Should(Equal(models.HostStatusKnown))
		Expect(*h2.Status).Should(Equal(models.HostStatusKnown))
		Expect(*h3.Status).Should(Equal(models.HostStatusKnown))

		// register new host with the same name in inventory
		By("Registering new host with same hostname as in node's inventory")
		newIPs := hostutil.GenerateIPv4Addresses(2, ips[2])
		h4 := registerNode(ctx, *infraEnvID, "h3", newIPs[0])
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4)
		h4 = getHostV2(*infraEnvID, *h4.ID)
		waitForHostState(ctx, models.HostStatusInsufficient, time.Minute, h3, h4)

		By("Check cluster install fails on validation")
		_, err = userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())

		By("Registering new host with same hostname as in node's requested_hostname")
		h5 := registerNode(ctx, *infraEnvID, "reqh0", newIPs[1])
		h5 = getHostV2(*infraEnvID, *h5.ID)
		generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4, h5)
		waitForHostState(ctx, models.HostStatusInsufficient, time.Minute, h1, h5)

		By("Change requested hostname of an insufficient node")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h5.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: swag.String("reqh0new"),
			},
			HostID:     *hosts[0].ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForHostState(ctx, models.HostStatusKnown, time.Minute, h1, h5)

		By("change the requested hostname of the insufficient node")
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(models.HostRoleWorker)),
			},
			HostID:     *h4.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostName: swag.String("reqh2"),
			},
			HostID:     *h3.ID,
			InfraEnvID: *infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, models.HostStatusKnown, time.Minute, h3)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, time.Minute, clusterReadyStateInfo)
		_, err = userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("Preflight Cluster Requirements", func() {
	var (
		ctx                   context.Context
		clusterID             strfmt.UUID
		masterOCPRequirements = models.ClusterHostRequirementsDetails{
			CPUCores:                         4,
			DiskSizeGb:                       100,
			RAMMib:                           16384,
			InstallationDiskSpeedThresholdMs: 10,
			NetworkLatencyThresholdMs:        ptr.To(float64(100)),
			PacketLossPercentage:             ptr.To(float64(0)),
		}
		workerOCPRequirements = models.ClusterHostRequirementsDetails{
			CPUCores:                         2,
			DiskSizeGb:                       100,
			RAMMib:                           8192,
			InstallationDiskSpeedThresholdMs: 10,
			NetworkLatencyThresholdMs:        ptr.To(float64(1000)),
			PacketLossPercentage:             ptr.To(float64(10)),
		}
		workerCNVRequirements = models.ClusterHostRequirementsDetails{
			CPUCores: 2,
			RAMMib:   360,
		}
		masterCNVRequirements = models.ClusterHostRequirementsDetails{
			CPUCores: 4,
			RAMMib:   150,
		}
		workerOCSRequirements = models.ClusterHostRequirementsDetails{
			CPUCores: 8,
			RAMMib:   conversions.GibToMib(19),
		}
		masterOCSRequirements = models.ClusterHostRequirementsDetails{
			CPUCores: 6,
			RAMMib:   conversions.GibToMib(19),
		}
		workerMCERequirements = models.ClusterHostRequirementsDetails{
			CPUCores: mce.MinimumCPU,
			RAMMib:   conversions.GibToMib(mce.MinimumMemory),
		}
		masterMCERequirements = models.ClusterHostRequirementsDetails{
			CPUCores: mce.MinimumCPU,
			RAMMib:   conversions.GibToMib(mce.MinimumMemory),
		}
	)

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())

		clusterID = cID
	})

	It("should be reported for cluster", func() {
		params := installer.V2GetPreflightRequirementsParams{ClusterID: clusterID}

		response, err := userBMClient.Installer.V2GetPreflightRequirements(ctx, &params)

		Expect(err).ToNot(HaveOccurred())
		requirements := response.GetPayload()

		expectedOcpRequirements := models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &masterOCPRequirements,
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &workerOCPRequirements,
			},
		}
		Expect(*requirements.Ocp).To(BeEquivalentTo(expectedOcpRequirements))
		Expect(requirements.Operators).To(HaveLen(5))
		for _, op := range requirements.Operators {
			switch op.OperatorName {
			case lso.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
			case odf.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterOCSRequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerOCSRequirements))
			case cnv.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterCNVRequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerCNVRequirements))
			case mce.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterMCERequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerMCERequirements))
			case lvm.Operator.Name:
				continue // lvm operator is tested separately
			default:
				Fail("Unexpected operator")
			}
		}
	})
})

var _ = Describe("Preflight Cluster Requirements for lvms", func() {
	var (
		ctx                             = context.Background()
		masterLVMRequirementsBefore4_13 = models.ClusterHostRequirementsDetails{
			CPUCores: 1,
			RAMMib:   1200,
		}
		masterLVMRequirements = models.ClusterHostRequirementsDetails{
			CPUCores: 1,
			RAMMib:   400,
		}
	)
	It("should be reported for 4.12 cluster", func() {
		var cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String("4.12.0"),
				PullSecret:        swag.String(pullSecret),
				BaseDNSDomain:     "example.com",
				VipDhcpAllocation: swag.Bool(true),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		clusterID := *cluster.GetPayload().ID
		params := installer.V2GetPreflightRequirementsParams{ClusterID: clusterID}

		response, err := userBMClient.Installer.V2GetPreflightRequirements(ctx, &params)
		Expect(err).ToNot(HaveOccurred())
		requirements := response.GetPayload()
		for _, op := range requirements.Operators {
			switch op.OperatorName {
			case lvm.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterLVMRequirementsBefore4_13))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
			}
		}
		_, err = userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should be reported for 4.13 cluster", func() {
		var cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String("4.13.0"),
				PullSecret:        swag.String(pullSecret),
				BaseDNSDomain:     "example.com",
				VipDhcpAllocation: swag.Bool(true),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		clusterID := *cluster.GetPayload().ID
		params := installer.V2GetPreflightRequirementsParams{ClusterID: clusterID}

		response, err := userBMClient.Installer.V2GetPreflightRequirements(ctx, &params)
		Expect(err).ToNot(HaveOccurred())
		requirements := response.GetPayload()
		for _, op := range requirements.Operators {
			switch op.OperatorName {
			case lvm.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterLVMRequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
			}
		}
		_, err = userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Multiple-VIPs Support", func() {
	var (
		ctx          = context.Background()
		cluster      *common.Cluster
		infraEnvID   *strfmt.UUID
		apiVip       = "1.2.3.8"
		ingressVip   = "1.2.3.9"
		apiVipv6     = "1003:db8::1"
		ingressVipv6 = "1003:db8::2"
		clusterCIDR  = "10.128.0.0/14"
		serviceCIDR  = "172.30.0.0/16"
	)

	AfterEach(func() {
		deregisterResources()
		clearDB()
	})

	setClusterIdForApiVips := func(apiVips []*models.APIVip, clusterID *strfmt.UUID) {
		for i := range apiVips {
			apiVips[i].ClusterID = *clusterID
		}
	}
	setClusterIdForIngressVips := func(ingressVips []*models.IngressVip, clusterID *strfmt.UUID) {
		for i := range ingressVips {
			ingressVips[i].ClusterID = *clusterID
		}
	}

	Context("V2RegisterCluster", func() {

		It("Two APIVips and Two IngressVips - both IPv4 - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.8")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.2")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Two APIVips and Two IngressVips - IPv4 first and IPv6 second - positive", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}

			reply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))

			cluster = &common.Cluster{Cluster: *reply.Payload}
			setClusterIdForApiVips(apiVips, cluster.ID)
			setClusterIdForIngressVips(ingressVips, cluster.ID)

			Expect(cluster.APIVips).To(Equal(apiVips))
			Expect(cluster.IngressVips).To(Equal(ingressVips))
		})

		It("Two APIVips and Two IngressVips - IPv6 first and IPv4 second - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVipv6)}, {IP: models.IP("8.8.8.7")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVipv6)}, {IP: models.IP("8.8.8.1")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Two APIVips and Two IngressVips - IPv6 - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVipv6)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVipv6)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("More than two APIVips and More than two IngressVips -  negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}, {IP: models.IP("8.8.8.3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}, {IP: models.IP("8.8.8.4")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Non parsable APIVips and non parsable IngressVips - negative", func() {
			apiVip = "1.1.1.300"
			ingressVip = "1.1.1.301"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("1.1.1.333")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("1.1.1.311")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Non parsable APIVips and parsable IngressVips - negative", func() {
			apiVip = "1.1.1.300"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Different number of APIVips and IngressVips - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Duplicated addresses in APIVips - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP(apiVip)}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::3")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Duplicated addresses in IngressVips - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP(ingressVip)}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})

		It("Duplicated address across APIVips and IngressVips - negative", func() {
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					APIVips:          apiVips,
					IngressVips:      ingressVips,
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})
	})

	Context("V2UpdateCluster", func() {

		BeforeEach(func() {
			reply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:    "example.com",
					ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(dualstackVipsOpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))

			cluster = &common.Cluster{Cluster: *reply.Payload}

			infraEnvID = registerInfraEnvSpecificVersion(cluster.ID, models.ImageTypeMinimalIso, cluster.OpenshiftVersion).ID
			_, _ = register3nodes(ctx, *cluster.ID, *infraEnvID, defaultCIDRv4)
		})

		AfterEach(func() {
			deregisterResources()
			clearDB()
		})

		It("Two APIVips and Two ingressVips - IPv6 first and IPv4 second - negative", func() {
			apiVip = "2001:db8::1"
			ingressVip = "2001:db8::2"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.7")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.1")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

		It("Two APIVips and Two ingressVips - IPv6 - negative", func() {
			apiVip = "2001:db8::1"
			ingressVip = "2001:db8::2"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

		It("More than two APIVips and More than two ingressVips -  negative", func() {
			apiVip = "1.2.3.100"
			ingressVip = "1.2.3.101"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}, {IP: models.IP("8.8.8.3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}, {IP: models.IP("8.8.8.4")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

		It("Non parsable APIVips and non parsable ingressVips - negative", func() {
			apiVip = "1.1.1.300"
			ingressVip = "1.1.1.301"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("1.1.1.333")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("1.1.1.311")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

		It("Non parsable APIVips and parsable ingressVips - negative", func() {
			apiVip = "1.1.1.300"
			ingressVip = "1.2.3.101"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

		It("Duplicated address across APIVips and ingressVips - negative", func() {
			apiVip = "1.2.3.100"
			apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
			ingressVips := []*models.IngressVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::4")}}

			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
					APIVips:           apiVips,
					IngressVips:       ingressVips,
				},
				ClusterID: *cluster.ID,
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterBadRequest()))
		})

	})

})

func checkUpdateAtWhileStatic(ctx context.Context, clusterID strfmt.UUID) {
	clusterReply, getErr := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
		ClusterID: clusterID,
	})
	Expect(getErr).ToNot(HaveOccurred())
	preSecondRefreshUpdatedTime := clusterReply.Payload.UpdatedAt
	time.Sleep(30 * time.Second)
	clusterReply, getErr = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
		ClusterID: clusterID,
	})
	Expect(getErr).ToNot(HaveOccurred())
	postRefreshUpdateTime := clusterReply.Payload.UpdatedAt
	Expect(preSecondRefreshUpdatedTime).Should(Equal(postRefreshUpdateTime))
}

const (
	masterFailure = iota
	workerFailure = iota
)

func FailCluster(ctx context.Context, clusterID, infraEnvID strfmt.UUID, reason int) strfmt.UUID {
	c := installCluster(clusterID)
	var hostID strfmt.UUID

	if reason == masterFailure {
		hostID = *getClusterMasters(c)[0].ID
	} else { // workerFailure when we only have 2 workers
		workers := getClusterWorkers(c)
		Expect(len(workers) == 2)
		hostID = *workers[0].ID
	}

	installStep := models.HostStageFailed
	installInfo := "because some error"

	updateHostProgressWithInfo(hostID, infraEnvID, installStep, installInfo)
	host := getHostV2(infraEnvID, hostID)
	Expect(*host.Status).Should(Equal("error"))
	Expect(*host.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", installStep, installInfo)))
	return hostID
}

var _ = Describe("cluster install, with default network params", func() {
	var (
		ctx        = context.Background()
		cluster    *models.Cluster
		infraEnvID *strfmt.UUID
	)

	BeforeEach(func() {
		By("Register cluster")
		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		infraEnvID = registerInfraEnv(cluster.ID, models.ImageTypeMinimalIso).ID
	})

	It("install cluster", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
		rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()
		startTimeInstalling := c.InstallStartedAt
		startTimeInstalled := c.InstallCompletedAt

		c = installCluster(clusterID)
		Expect(len(c.Hosts)).Should(Equal(5))
		Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		waitForHostState(ctx, "installing", 10*time.Second, c.Hosts...)

		// fake installation completed
		for _, host := range c.Hosts {
			updateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
		}

		waitForClusterState(ctx, clusterID, "finalizing", defaultWaitForClusterStateTimeout, "Finalizing cluster installation")
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)

		rep, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		c = rep.GetPayload()
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalled))
		Expect(c.InstallCompletedAt).ShouldNot(Equal(startTimeInstalled))
		Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
	})

	Context("fail disk speed", func() {
		It("first host", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
			rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, c.Hosts[1:], c.Hosts[:1])
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
		It("all hosts", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
			rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, nil, c.Hosts)
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
		It("last host", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)
			rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, nil, c.Hosts[len(c.Hosts)-1:])
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
	})
})

func registerHostsAndSetRoles(clusterID, infraenvID strfmt.UUID, numHosts int, clusterName string, baseDNSDomain string) []*models.Host {
	ctx := context.Background()
	hosts := make([]*models.Host, 0)

	ips := hostutil.GenerateIPv4Addresses(numHosts, defaultCIDRv4)
	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := registerNode(ctx, infraenvID, hostname, ips[i])
		var role models.HostRole
		if i < 3 {
			role = models.HostRoleMaster
		} else {
			role = models.HostRoleWorker
		}
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(role)),
			},
			HostID:     *host.ID,
			InfraEnvID: infraenvID,
		})
		Expect(err).NotTo(HaveOccurred())
		hosts = append(hosts, host)
	}
	for _, host := range hosts {
		generateDomainResolution(ctx, host, clusterName, baseDNSDomain)
		generateCommonDomainReply(ctx, host, clusterName, baseDNSDomain)
	}
	generateFullMeshConnectivity(ctx, ips[0], hosts...)
	cluster := getCluster(clusterID)
	if cluster.DiskEncryption != nil && swag.StringValue(cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTang {
		generateTangPostStepReply(ctx, true, hosts...)
	}

	if !swag.BoolValue(cluster.UserManagedNetworking) {
		_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
				APIVips:           []*models.APIVip{},
				IngressVips:       []*models.IngressVip{},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		apiVip := "1.2.3.8"
		ingressVip := "1.2.3.9"
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				APIVips:     []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
				IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
			},
			ClusterID: clusterID,
		})

		Expect(err).NotTo(HaveOccurred())
	}

	waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, hosts...)
	waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

	return hosts
}

func registerHostsAndSetRolesTang(clusterID, infraenvID strfmt.UUID, numHosts int, clusterName string, baseDNSDomain string, tangValidated bool) []*models.Host {
	ctx := context.Background()
	hosts := make([]*models.Host, 0)

	ips := hostutil.GenerateIPv4Addresses(numHosts, defaultCIDRv4)
	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := registerNode(ctx, infraenvID, hostname, ips[i])
		var role models.HostRole
		if i < 3 {
			role = models.HostRoleMaster
		} else {
			role = models.HostRoleWorker
		}
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(role)),
			},
			HostID:     *host.ID,
			InfraEnvID: infraenvID,
		})
		Expect(err).NotTo(HaveOccurred())
		hosts = append(hosts, host)
	}
	for _, host := range hosts {
		generateDomainResolution(ctx, host, clusterName, baseDNSDomain)
	}
	generateFullMeshConnectivity(ctx, ips[0], hosts...)
	cluster := getCluster(clusterID)
	if cluster.DiskEncryption != nil && swag.StringValue(cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTang {
		generateTangPostStepReply(ctx, tangValidated, hosts...)
	}

	_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVips:           []*models.APIVip{},
			IngressVips:       []*models.IngressVip{},
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	apiVip := "1.2.3.8"
	ingressVip := "1.2.3.9"
	_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			APIVips:     []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
			IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
		},
		ClusterID: clusterID,
	})

	Expect(err).NotTo(HaveOccurred())

	waitForHostState(ctx, models.HostStatusKnown, defaultWaitForHostStateTimeout, hosts...)
	waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

	return hosts
}

func registerHostsAndSetRolesDHCP(clusterID, infraEnvID strfmt.UUID, numHosts int, clusterName string, baseDNSDomain string) []*models.Host {
	ctx := context.Background()
	hosts := make([]*models.Host, 0)
	apiVip := "1.2.3.8"
	ingressVip := "1.2.3.9"

	generateDhcpStepReply := func(h *models.Host, apiVip, ingressVip string) {
		avip := strfmt.IPv4(apiVip)
		ivip := strfmt.IPv4(ingressVip)
		r := models.DhcpAllocationResponse{
			APIVipAddress:     &avip,
			IngressVipAddress: &ivip,
		}
		b, err := json.Marshal(&r)
		Expect(err).ToNot(HaveOccurred())
		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: h.InfraEnvID,
			HostID:     *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				StepType: models.StepTypeDhcpLeaseAllocate,
				Output:   string(b),
				StepID:   string(models.StepTypeDhcpLeaseAllocate),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}
	ips := hostutil.GenerateIPv4Addresses(numHosts, defaultCIDRv4)
	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := registerNode(ctx, infraEnvID, hostname, ips[i])
		var role models.HostRole
		if i < 3 {
			role = models.HostRoleMaster
		} else {
			role = models.HostRoleWorker
		}
		_, err := userBMClient.Installer.V2UpdateHost(ctx, &installer.V2UpdateHostParams{
			HostUpdateParams: &models.HostUpdateParams{
				HostRole: swag.String(string(role)),
			},
			HostID:     *host.ID,
			InfraEnvID: infraEnvID,
		})
		Expect(err).NotTo(HaveOccurred())
		hosts = append(hosts, host)
	}
	generateFullMeshConnectivity(ctx, ips[0], hosts...)
	_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
		},
		ClusterID: clusterID,
	})
	Expect(err).ToNot(HaveOccurred())
	for _, h := range hosts {
		generateDhcpStepReply(h, apiVip, ingressVip)
		generateDomainResolution(ctx, h, clusterName, baseDNSDomain)
	}
	waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

	return hosts
}

func getClusterMasters(c *models.Cluster) (masters []*models.Host) {
	for _, host := range c.Hosts {
		if host.Role == models.HostRoleMaster {
			masters = append(masters, host)
		}
	}

	return
}

func getClusterWorkers(c *models.Cluster) (workers []*models.Host) {
	for _, host := range c.Hosts {
		if host.Role == models.HostRoleWorker {
			workers = append(workers, host)
		}
	}

	return
}

func generateConnectivityPostStepReply(ctx context.Context, h *models.Host, connectivityReport *models.ConnectivityReport) {
	fa, err := json.Marshal(connectivityReport)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(fa),
			StepID:   string(models.StepTypeConnectivityCheck),
			StepType: models.StepTypeConnectivityCheck,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func generateFullMeshConnectivity(ctx context.Context, startCIDR string, hosts ...*models.Host) {

	ip, _, err := net.ParseCIDR(startCIDR)
	Expect(err).NotTo(HaveOccurred())
	hostToAddr := make(map[strfmt.UUID]string)

	for _, h := range hosts {
		hostToAddr[*h.ID] = ip.String()
		common.IncrementIP(ip)
	}

	var connectivityReport models.ConnectivityReport
	for _, h := range hosts {

		l2Connectivity := make([]*models.L2Connectivity, 0)
		l3Connectivity := make([]*models.L3Connectivity, 0)
		for id, addr := range hostToAddr {

			if id != *h.ID {
				continue
			}

			l2Connectivity = append(l2Connectivity, &models.L2Connectivity{
				RemoteIPAddress: addr,
				Successful:      true,
			})
			l3Connectivity = append(l3Connectivity, &models.L3Connectivity{
				RemoteIPAddress: addr,
				Successful:      true,
			})
		}

		connectivityReport.RemoteHosts = append(connectivityReport.RemoteHosts, &models.ConnectivityRemoteHost{
			HostID:         *h.ID,
			L2Connectivity: l2Connectivity,
			L3Connectivity: l3Connectivity,
		})
	}

	for _, h := range hosts {
		generateConnectivityPostStepReply(ctx, h, &connectivityReport)
	}
}

func expectProgressToBe(c *models.Cluster, preparingForInstallationStagePercentage, installingStagePercentage, finalizingStagePercentage int) {

	preparingForInstallationRange := []int{preparingForInstallationStagePercentage, preparingForInstallationStagePercentage}
	installingRange := []int{installingStagePercentage, installingStagePercentage}
	finalizingRange := []int{finalizingStagePercentage, finalizingStagePercentage}
	expectProgressToBeInRange(c, preparingForInstallationRange, installingRange, finalizingRange)
}

func expectProgressToBeInRange(c *models.Cluster, preparingForInstallationRange, installingRange, finalizingRange []int) {
	if c.Progress == nil {
		c.Progress = &models.ClusterProgressInfo{}
	}
	Expect(c.Progress.PreparingForInstallationStagePercentage >= int64(preparingForInstallationRange[0]) &&
		c.Progress.PreparingForInstallationStagePercentage <= int64(preparingForInstallationRange[1])).To(BeTrue())
	Expect(c.Progress.InstallingStagePercentage >= int64(installingRange[0]) &&
		c.Progress.InstallingStagePercentage <= int64(installingRange[1])).To(BeTrue())
	Expect(c.Progress.FinalizingStagePercentage >= int64(finalizingRange[0]) &&
		c.Progress.FinalizingStagePercentage <= int64(finalizingRange[1])).To(BeTrue())
	totalPercentage := common.ProgressWeightPreparingForInstallationStage*float64(c.Progress.PreparingForInstallationStagePercentage) +
		common.ProgressWeightInstallingStage*float64(c.Progress.InstallingStagePercentage) +
		common.ProgressWeightFinalizingStage*float64(c.Progress.FinalizingStagePercentage)
	Expect(c.Progress.TotalPercentage).To(Equal(int64(totalPercentage)))
}

var _ = Describe("Cluster registration default", func() {
	var (
		ctx         = context.Background()
		c           *models.Cluster
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)
	It("RegisterCluster", func() {
		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		c = registerClusterReply.GetPayload()
		Expect(c.VipDhcpAllocation).To(Equal(swag.Bool(false)))
	})
})

var _ = Describe("Installation progress", func() {
	var (
		ctx         = context.Background()
		c           *models.Cluster
		infraEnvID  *strfmt.UUID
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)

	It("Test installation progress", func() {

		By("register cluster", func() {

			// register cluster
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:     "example.com",
					ClusterNetworks:   []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:   []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:              swag.String("test-cluster"),
					OpenshiftVersion:  swag.String(VipAutoAllocOpenshiftVersion),
					PullSecret:        swag.String(pullSecret),
					SSHPublicKey:      sshPublicKey,
					NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
					VipDhcpAllocation: swag.Bool(true),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = registerClusterReply.GetPayload()

			// add hosts

			infraEnvID = registerInfraEnv(c.ID, models.ImageTypeMinimalIso).ID
			registerHostsAndSetRolesDHCP(*c.ID, *infraEnvID, 6, "test-cluster", "example.com")

			// add OLM operators

			updateClusterReply, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					OlmOperators: []*models.OperatorCreateParams{
						{Name: lso.Operator.Name},
						{Name: odf.Operator.Name},
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			c = updateClusterReply.GetPayload()

			expectProgressToBe(c, 0, 0, 0)
		})

		By("preparing-for-installation stage", func() {

			c = installCluster(*c.ID)
			expectProgressToBe(c, 100, 0, 0)
		})

		By("installing stage - report hosts' progress", func() {

			// intermediate report

			for _, h := range c.Hosts {
				updateProgress(*h.ID, h.InfraEnvID, models.HostStageWritingImageToDisk)
			}
			c = getCluster(*c.ID)

			expectProgressToBeInRange(c, []int{100, 100}, []int{1, 50}, []int{0, 0})

			// last report

			for _, h := range c.Hosts {
				updateProgress(*h.ID, h.InfraEnvID, models.HostStageDone)
			}
			c = getCluster(*c.ID)

			expectProgressToBe(c, 100, 100, 0)
		})

		By("finalizing stage - report operators' progress", func() {

			waitForClusterState(ctx, *c.ID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)

			v2ReportMonitoredOperatorStatus(ctx, agentBMClient, *c.ID, operators.OperatorConsole.Name, models.OperatorStatusAvailable, "")
			c = getCluster(*c.ID)
			expectProgressToBe(c, 100, 100, 33)

			v2ReportMonitoredOperatorStatus(ctx, agentBMClient, *c.ID, lso.Operator.Name, models.OperatorStatusAvailable, "")
			c = getCluster(*c.ID)
			expectProgressToBe(c, 100, 100, 66)

			v2ReportMonitoredOperatorStatus(ctx, agentBMClient, *c.ID, odf.Operator.Name, models.OperatorStatusFailed, "")
			c = getCluster(*c.ID)
			expectProgressToBe(c, 100, 100, 100)
		})
	})
})

var _ = Describe("disk encryption", func() {

	var (
		ctx        = context.Background()
		c          *models.Cluster
		infraEnvID *strfmt.UUID
	)
	Context("DiskEncryption mode: "+models.DiskEncryptionModeTpmv2, func() {

		BeforeEach(func() {
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(VipAutoAllocOpenshiftVersion),
					PullSecret:       swag.String(pullSecret),
					SSHPublicKey:     sshPublicKey,
					BaseDNSDomain:    "example.com",
					DiskEncryption: &models.DiskEncryption{
						EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
						Mode:     swag.String(models.DiskEncryptionModeTpmv2),
					},
					VipDhcpAllocation: swag.Bool(true),
					NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = registerClusterReply.GetPayload()
			infraEnvID = registerInfraEnv(c.ID, models.ImageTypeMinimalIso).ID

			// validate feature usage
			var featureUsage map[string]models.Usage
			err = json.Unmarshal([]byte(c.FeatureUsage), &featureUsage)
			Expect(err).NotTo(HaveOccurred())
			Expect(featureUsage["Disk encryption"].Data["enable_on"]).To(Equal(models.DiskEncryptionEnableOnAll))
			Expect(featureUsage["Disk encryption"].Data["mode"]).To(Equal(models.DiskEncryptionModeTpmv2))
			Expect(featureUsage["Disk encryption"].Data["tang_servers"]).To(BeEmpty())
		})

		It("happy flow", func() {
			registerHostsAndSetRolesDHCP(*c.ID, *infraEnvID, 3, "test-cluster", "example.com")

			reply, err := userBMClient.Installer.V2InstallCluster(ctx, &installer.V2InstallClusterParams{ClusterID: *c.ID})
			Expect(err).NotTo(HaveOccurred())
			c = reply.GetPayload()

			generateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)
			waitForLastInstallationCompletionStatus(*c.ID, models.LastInstallationPreparationStatusSuccess)
		})

		It("host doesn't have minimal requirements for disk-encryption, TPM mode", func() {
			h := &registerHost(*infraEnvID).Host
			nonValidTPMHwInfo := &models.Inventory{
				CPU:    &models.CPU{Count: 16},
				Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
				Disks:  []*models.Disk{&loop0, &sdb},
				Interfaces: []*models.Interface{
					{
						IPV4Addresses: []string{
							defaultCIDRv4,
						},
						MacAddress: "e6:53:3d:a7:77:b4",
						Type:       "physical",
					},
				},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
				Routes:       common.TestDefaultRouteConfiguration,
				TpmVersion:   models.InventoryTpmVersionNr12,
			}
			generateEssentialHostStepsWithInventory(ctx, h, "test-host", nonValidTPMHwInfo)
			time.Sleep(60 * time.Second)
			waitForHostState(ctx, models.HostStatusInsufficient, 60*time.Second, h)

			h = getHostV2(*infraEnvID, *h.ID)
			Expect(*h.StatusInfo).Should(ContainSubstring("The host's TPM version is not supported"))
		})
	})

	Context("DiskEncryption mode: "+models.DiskEncryptionModeTang, func() {

		BeforeEach(func() {
			tangServers := `[{"URL":"http://tang.example.com:7500","Thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:     "example.com",
					Name:              swag.String("test-cluster"),
					OpenshiftVersion:  swag.String(VipAutoAllocOpenshiftVersion),
					PullSecret:        swag.String(pullSecret),
					SSHPublicKey:      sshPublicKey,
					VipDhcpAllocation: swag.Bool(true),
					DiskEncryption: &models.DiskEncryption{
						EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
						Mode:        swag.String(models.DiskEncryptionModeTang),
						TangServers: tangServers,
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			c = registerClusterReply.GetPayload()
			infraEnvID = registerInfraEnvSpecificVersion(c.ID, models.ImageTypeMinimalIso, c.OpenshiftVersion).ID

			// validate feature usage
			var featureUsage map[string]models.Usage
			err = json.Unmarshal([]byte(c.FeatureUsage), &featureUsage)
			Expect(err).NotTo(HaveOccurred())
			Expect(featureUsage["Disk encryption"].Data["enable_on"]).To(Equal(models.DiskEncryptionEnableOnAll))
			Expect(featureUsage["Disk encryption"].Data["mode"]).To(Equal(models.DiskEncryptionModeTang))
			Expect(featureUsage["Disk encryption"].Data["tang_servers"]).To(Equal(tangServers))
		})

		It("install cluster - happy flow", func() {
			clusterID := *c.ID
			registerHostsAndSetRolesTang(clusterID, *infraEnvID, 5, c.Name, c.BaseDNSDomain, true)
			rep, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c = rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt
			startTimeInstalled := c.InstallCompletedAt

			c = installCluster(clusterID)
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
			waitForHostState(ctx, "installing", 10*time.Second, c.Hosts...)

			// fake installation completed
			for _, host := range c.Hosts {
				updateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
			}

			waitForClusterState(ctx, clusterID, "finalizing", defaultWaitForClusterStateTimeout, "Finalizing cluster installation")
			completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)

			rep, err = userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})

			Expect(err).NotTo(HaveOccurred())
			c = rep.GetPayload()
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalled))
			Expect(c.InstallCompletedAt).ShouldNot(Equal(startTimeInstalled))
			Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
		})

		It("host fails tang connectivity validation", func() {
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
						Type:       "physical",
					},
				},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "RHEL", SerialNumber: "3534"},
				Routes:       common.TestDefaultRouteConfiguration,
				TpmVersion:   models.InventoryTpmVersionNr20,
			}

			h := &registerHost(*infraEnvID).Host

			generateEssentialHostStepsWithInventory(ctx, h, "test-host", inventoryBMInfo)
			generateTangPostStepReply(ctx, false, h)
			time.Sleep(60 * time.Second)
			waitForHostState(ctx, models.HostStatusInsufficient, 60*time.Second, h)

			h = getHostV2(*infraEnvID, *h.ID)
			Expect(*h.StatusInfo).Should(ContainSubstring("Could not validate that all Tang servers are reachable and working"))
		})
	})
})

var _ = Describe("Verify install-config manifest", func() {
	var (
		ctx         = context.Background()
		cluster     *models.Cluster
		clusterID   strfmt.UUID
		infraEnvID  *strfmt.UUID
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
		machineCIDR = "1.2.3.0/24"
	)

	getInstallConfigFromFile := func() map[string]interface{} {
		file, err := os.CreateTemp("", "tmp")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(file.Name())

		By("Download install-config.yaml")
		_, err = userBMClient.Installer.V2DownloadClusterFiles(ctx,
			&installer.V2DownloadClusterFilesParams{
				ClusterID: clusterID,
				FileName:  "install-config.yaml",
			}, file)
		Expect(err).NotTo(HaveOccurred())

		// Read install-config.yaml
		content, err := os.ReadFile(file.Name())
		Expect(err).NotTo(HaveOccurred())

		installConfig := make(map[string]interface{})
		err = yaml.Unmarshal(content, installConfig)
		Expect(err).NotTo(HaveOccurred())

		return installConfig
	}

	getInstallConfigFromDB := func() map[string]interface{} {
		response, err := userBMClient.Installer.V2GetClusterInstallConfig(ctx,
			&installer.V2GetClusterInstallConfigParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		installConfig := make(map[string]interface{})
		err = yaml.Unmarshal([]byte(response.Payload), installConfig)
		Expect(err).NotTo(HaveOccurred())

		return installConfig
	}

	validateInstallConfig := func(installConfig map[string]interface{}, platformType models.PlatformType) {
		By("Validate 'baseDomain'")
		baseDomain, ok := installConfig["baseDomain"].(string)
		Expect(ok).To(Equal(true))
		Expect(baseDomain).To(Equal(cluster.BaseDNSDomain))

		By("Validate 'metadata'")
		metadata, ok := installConfig["metadata"].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))
		name, ok := metadata["name"].(string)
		Expect(ok).To(Equal(true))
		Expect(name).To(Equal(cluster.Name))

		By("Validate 'platform'")
		platform, ok := installConfig["platform"].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))
		_, ok = platform[string(platformType)].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))

		By("Validate 'pullSecret'")
		ps, ok := installConfig["pullSecret"].(string)
		Expect(ok).To(Equal(true))
		Expect(ps).To(Equal(pullSecret))

		By("Validate 'sshKey'")
		sshKey, ok := installConfig["sshKey"].(string)
		Expect(ok).To(Equal(true))
		Expect(sshKey).To(Equal(sshPublicKey))

		By("Validate 'networking'")
		networking, ok := installConfig["networking"].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))

		// Validate 'clusterNetwork'
		clusterNetwork, ok := networking["clusterNetwork"].([]interface{})
		Expect(ok).To(Equal(true))
		cidrEntry, ok := clusterNetwork[0].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))
		cidr, ok := cidrEntry["cidr"].(string)
		Expect(ok).To(Equal(true))
		Expect(cidr).To(Equal(clusterCIDR))
		// Validate 'machineNetwork'
		machineNetwork, ok := networking["machineNetwork"].([]interface{})
		Expect(ok).To(Equal(true))
		cidrEntry, ok = machineNetwork[0].(map[interface{}]interface{})
		Expect(ok).To(Equal(true))
		cidr, ok = cidrEntry["cidr"].(string)
		Expect(ok).To(Equal(true))
		Expect(cidr).To(Equal(machineCIDR))
		// Validate 'serviceNetwork'
		serviceNetwork, ok := networking["serviceNetwork"].([]interface{})
		Expect(ok).To(Equal(true))
		cidr, ok = serviceNetwork[0].(string)
		Expect(ok).To(Equal(true))
		Expect(cidr).To(Equal(serviceCIDR))

		if cluster.InstallConfigOverrides != "" {
			// Ensure config override (with 'fips' for example)
			fips, ok := installConfig["fips"].(bool)
			Expect(ok).To(Equal(true))
			Expect(fips).To(Equal(true))
		}
	}

	installCluster := func(platformType models.PlatformType, overrideInstallConfig bool) {
		registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:    "example.com",
				ClusterNetworks:  []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
				ServiceNetworks:  []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(SDNNetworkTypeOpenshiftVersion),
				PullSecret:       swag.String(pullSecret),
				SSHPublicKey:     sshPublicKey,
				NetworkType:      swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
				Platform:         &models.Platform{Type: common.PlatformTypePtr(platformType)},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		clusterID = *cluster.ID

		// Update install config if needed
		if overrideInstallConfig {
			params := installer.V2UpdateClusterInstallConfigParams{
				ClusterID:           clusterID,
				InstallConfigParams: `{"fips": true}`,
			}
			_, err = userBMClient.Installer.V2UpdateClusterInstallConfig(ctx, &params)
			Expect(err).To(BeNil())
		}

		// Register InfraEnv and Hosts
		infraEnvID = registerInfraEnv(cluster.ID, models.ImageTypeMinimalIso).ID
		registerHostsAndSetRoles(clusterID, *infraEnvID, 5, cluster.Name, cluster.BaseDNSDomain)

		// Installing cluster till finalize
		setClusterAsFinalizing(ctx, clusterID)

		// Completing cluster installation
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
	}

	AfterEach(func() {
		deregisterResources()
		clearDB()
	})

	DescribeTable("Validate install-config content", func(
		operationName string,
		getInstallConfigFunc func() map[string]interface{},
		platformType models.PlatformType,
		overrideInstallConfig bool,
	) {
		installCluster(platformType, overrideInstallConfig)
		validateInstallConfig(getInstallConfigFunc(), platformType)
	},
		Entry("Operation: V2DownloadClusterFiles, Platfrom type: baremetal", "V2DownloadClusterFiles", getInstallConfigFromDB, models.PlatformTypeBaremetal, false),
		Entry("Operation: V2DownloadClusterFiles, Platfrom type: none", "V2DownloadClusterFiles", getInstallConfigFromDB, models.PlatformTypeNone, false),
		Entry("Operation: V2DownloadClusterFiles, Override config: true", "V2DownloadClusterFiles", getInstallConfigFromDB, models.PlatformTypeBaremetal, true),
		Entry("Operation: V2GetClusterInstallConfig, Platfrom type: baremetal", "V2GetClusterInstallConfig", getInstallConfigFromFile, models.PlatformTypeBaremetal, false),
		Entry("Operation: V2GetClusterInstallConfig, Platfrom type: none", "V2GetClusterInstallConfig", getInstallConfigFromFile, models.PlatformTypeNone, false),
		Entry("Operation: V2GetClusterInstallConfig, Override config: true", "V2GetClusterInstallConfig", getInstallConfigFromFile, models.PlatformTypeBaremetal, true),
	)
})
