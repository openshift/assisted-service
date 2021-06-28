package subsystem

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/conversions"
	"k8s.io/utils/pointer"
)

// #nosec
const (
	clusterInsufficientStateInfo                = "Cluster is not ready for install"
	clusterReadyStateInfo                       = "Cluster ready to be installed"
	sshPublicKey                                = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain"
	pullSecretName                              = "pull-secret"
	pullSecret                                  = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
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

	validWorkerHwInfo = &models.Inventory{
		CPU:    &models.CPU{Count: 2},
		Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB), UsableBytes: int64(8 * units.GiB)},
		Disks:  []*models.Disk{&loop0, &sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				MacAddress: "e6:53:3d:a7:77:b4",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Timestamp:    1601853088,
		Routes:       common.TestDefaultRouteConfiguration,
	}
	validHwInfo = &models.Inventory{
		CPU:    &models.CPU{Count: 16},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
		Disks:  []*models.Disk{&loop0, &sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				MacAddress: "e6:53:3d:a7:77:b4",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Timestamp:    1601853088,
		Routes:       common.TestDefaultRouteConfiguration,
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

var _ = Describe("Cluster", func() {
	ctx := context.Background()
	var cluster *installer.RegisterClusterCreated
	var clusterID strfmt.UUID
	var err error
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
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
		h := registerHost(clusterID)
		_, err1 := userBMClient.Installer.DeregisterHost(ctx, &installer.DeregisterHostParams{
			ClusterID: clusterID,
			HostID:    *h.ID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		_, err2 := agentBMClient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
			ClusterID: clusterID,
			NewHostParams: &models.HostCreateParams{
				HostID: h.ID,
			},
		})
		Expect(err2).ShouldNot(HaveOccurred())
		c := getCluster(clusterID)
		Expect(len(c.Hosts)).Should(Equal(1))
		Expect(c.Hosts[0].ID.String()).Should(Equal(h.ID.String()))
	})

	It("update cluster name exceed max length (54 characters)", func() {
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				Name: swag.String("loveisintheaireverywhereilookaroundloveisintheaireverysightandeverysound"),
			},
			ClusterID: clusterID,
		})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster name exceed max length (54 characters)", func() {
		_, err1 := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
			ClusterID: clusterID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).Should(HaveOccurred())
	})

	It("register an unregistered cluster success", func() {
		_, err1 := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
			ClusterID: clusterID,
		})
		Expect(err1).ShouldNot(HaveOccurred())
		cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
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
		_ = registerHost(clusterID)
		_, err1 := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err1).ShouldNot(HaveOccurred())
		ret, err2 := readOnlyAdminUserBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
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
		_ = registerHost(clusterID)
		_, err1 := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err1).ShouldNot(HaveOccurred())
		ret, err2 := readOnlyAdminUserBMClient.Installer.ListClusters(ctx,
			&installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true),
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
		_ = registerHost(clusterID)
		Expect(err).NotTo(HaveOccurred())

		getReply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		getReply, err = agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		By("update cluster with valid ssh key")
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)

		validPublicKey := sshPublicKey

		c, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: &validPublicKey,
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{
						ID:   *host1.ID,
						Role: models.HostRoleUpdateParamsMaster,
					},
					{
						ID:   *host2.ID,
						Role: models.HostRoleUpdateParamsWorker,
					},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(validPublicKey))

		h := getHost(clusterID, *host1.ID)
		Expect(h.Role).Should(Equal(models.HostRole(models.HostRoleUpdateParamsMaster)))

		h = getHost(clusterID, *host2.ID)
		Expect(h.Role).Should(Equal(models.HostRole(models.HostRoleUpdateParamsWorker)))

		By("update cluster invalid ssh key")
		invalidPublicKey := `ssh-rsa AAAAB3NzaC1yc2EAAAADAABgQD14Gv4V5DVvyr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUAAbfmn/HalJTHB73LyjilKiPvJ+x2bG7AeiqyVHwtQSpt02FCdQGptmsSqqWF/b9botOO38eUsqPNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= oscohen@localhost.localdomain`

		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: &invalidPublicKey,
			},
			ClusterID: clusterID,
		})
		Expect(err).Should(HaveOccurred())
	})
})

func isClusterInState(ctx context.Context, clusterID strfmt.UUID, state, stateInfo string) (bool, string, string) {
	rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := rep.GetPayload()
	if swag.StringValue(c.Status) == state {
		return stateInfo == IgnoreStateInfo ||
			swag.StringValue(c.StatusInfo) == stateInfo, swag.StringValue(c.Status), swag.StringValue(c.StatusInfo)
	}
	Expect(swag.StringValue(c.Status)).NotTo(Equal("error"))

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

	Expect(lastState).Should(Equal(state), fmt.Sprintf("Cluster %s wasn't in state %s for %d times in a row. Actual %s (%s)",
		clusterID, state, minSuccessesInRow, lastState, lastStatusInfo))
}

func isHostInState(ctx context.Context, clusterID strfmt.UUID, hostID strfmt.UUID, state string) (bool, string, string) {
	rep, err := userBMClient.Installer.GetHost(ctx, &installer.GetHostParams{ClusterID: clusterID, HostID: hostID})
	Expect(err).NotTo(HaveOccurred())
	h := rep.GetPayload()
	return swag.StringValue(h.Status) == state, swag.StringValue(h.Status), swag.StringValue(h.StatusInfo)
}

func waitForHostState(ctx context.Context, clusterID strfmt.UUID, hostID strfmt.UUID, state string, timeout time.Duration) {
	log.Infof("Waiting for host %s state %s", hostID, state)
	var (
		lastState      string
		lastStatusInfo string
		success        bool
	)

	for start, successInRow := time.Now(), 0; time.Since(start) < timeout; {
		success, lastState, lastStatusInfo = isHostInState(ctx, clusterID, hostID, state)

		if success {
			successInRow++
		} else {
			successInRow = 0
		}

		// Wait for host state to be consistent
		if successInRow >= minSuccessesInRow {
			log.Infof("host %s has status %s", clusterID, state)
			return
		}

		time.Sleep(time.Second)
	}

	Expect(lastState).Should(Equal(state), fmt.Sprintf("Host %s in Cluster %s wasn't in state %s for %d times in a row. Actual %s (%s)",
		hostID, clusterID, state, minSuccessesInRow, lastState, lastStatusInfo))
}

func waitForMachineNetworkCIDR(
	ctx context.Context, clusterID strfmt.UUID, machineNetworkCIDR string, timeout time.Duration) error {

	log.Infof("Waiting for cluster=%s to have machineNetworkCIDR=%s", clusterID, machineNetworkCIDR)

	currentMachineNetworkCIDR := ""
	for start, _ := time.Now(), 0; time.Since(start) < timeout; {
		rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()
		currentMachineNetworkCIDR = c.MachineNetworkCidr

		if currentMachineNetworkCIDR == machineNetworkCIDR {
			log.Infof("cluster=%s has machineNetworkCIDR=%s", clusterID, machineNetworkCIDR)
			return nil
		}

		time.Sleep(time.Second)
	}

	return fmt.Errorf("cluster=%s has machineNetworkCIDR=%s but expected=%s",
		clusterID, currentMachineNetworkCIDR, machineNetworkCIDR)
}

func installCluster(clusterID strfmt.UUID) *models.Cluster {
	ctx := context.Background()
	reply, err := userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := reply.GetPayload()
	Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
	generateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)

	waitForClusterState(ctx, clusterID, models.ClusterStatusInstalling,
		180*time.Second, "Installation in progress")

	for _, host := range c.Hosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled {
			waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling, defaultWaitForHostStateTimeout)
		}
	}

	rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c = rep.GetPayload()
	Expect(c).NotTo(BeNil())

	return c
}

func tryInstallClusterWithDiskResponses(clusterID strfmt.UUID, successfulHosts, failedHosts []*models.Host) *models.Cluster {
	Expect(len(failedHosts)).To(BeNumerically(">", 0))
	ctx := context.Background()
	reply, err := userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c := reply.GetPayload()
	Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
	generateFailedDiskSpeedResponses(ctx, sdbId, failedHosts...)
	generateSuccessfulDiskSpeedResponses(ctx, sdbId, successfulHosts...)

	waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient,
		180*time.Second, IgnoreStateInfo)

	for _, host := range failedHosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled {
			waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInsufficient, defaultWaitForHostStateTimeout)
		}
	}

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

	for _, host := range expectedKnownHosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled {
			waitForHostState(ctx, clusterID, *host.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
		}
	}
	rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())
	c = rep.GetPayload()
	Expect(c).NotTo(BeNil())

	return c
}

func completeInstallation(client *client.AssistedInstall, clusterID strfmt.UUID) {
	ctx := context.Background()
	rep, err := client.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())

	status := models.OperatorStatusAvailable

	Eventually(func() error {
		_, err = agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: models.IngressCertParams(ingressCa),
		})
		return err
	}, "10s", "2s").Should(BeNil())

	for _, operator := range rep.Payload.MonitoredOperators {
		if operator.OperatorType != models.OperatorTypeBuiltin {
			continue
		}

		reportMonitoredOperatorStatus(ctx, client, clusterID, operator.Name, status)
	}
}

func failInstallation(client *client.AssistedInstall, clusterID strfmt.UUID) {
	ctx := context.Background()
	isSuccess := false
	_, err := client.Installer.CompleteInstallation(ctx, &installer.CompleteInstallationParams{
		ClusterID: clusterID,
		CompletionParams: &models.CompletionParams{
			IsSuccess: &isSuccess,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

func completeInstallationAndVerify(ctx context.Context, client *client.AssistedInstall, clusterID strfmt.UUID, completeSuccess bool) {
	expectedStatus := models.ClusterStatusError

	if completeSuccess {
		completeInstallation(client, clusterID)
		expectedStatus = models.ClusterStatusInstalled
	} else {
		failInstallation(client, clusterID)
	}

	waitForClusterState(ctx, clusterID, expectedStatus, defaultWaitForClusterStateTimeout, IgnoreStateInfo)
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
		updateProgress(*host.ID, clusterID, models.HostStageDone)
	}

	waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
}

var _ = Describe("ListClusters", func() {

	var (
		ctx     = context.Background()
		cluster *models.Cluster
	)

	BeforeEach(func() {

		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				SSHPublicKey:             sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		log.Infof("Register cluster %s", cluster.ID.String())
	})

	AfterEach(func() {
		clearDB()
	})

	Context("Filter by opensfhift cluster ID", func() {

		BeforeEach(func() {
			registerHostsAndSetRolesDHCP(*cluster.ID, 5)
			_ = installCluster(*cluster.ID)
		})

		It("searching for an existing openshift cluster ID", func() {
			list, err := userBMClient.Installer.ListClusters(
				ctx,
				&installer.ListClustersParams{OpenshiftClusterID: strToUUID("41940ee8-ec99-43de-8766-174381b4921d")})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("discarding openshift cluster ID field", func() {
			list, err := userBMClient.Installer.ListClusters(
				ctx,
				&installer.ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("searching for a non-existing openshift cluster ID", func() {
			list, err := userBMClient.Installer.ListClusters(
				ctx,
				&installer.ListClustersParams{OpenshiftClusterID: strToUUID("00000000-0000-0000-0000-000000000000")})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(0))
		})
	})

	Context("Filter by AMS subscription IDs", func() {

		BeforeEach(func() {
			if Options.AuthType == auth.TypeNone {
				Skip("auth is disabled")
			}
			if !Options.WithAMSSubscriptions {
				Skip("AMS is disabled")
			}
		})

		It("searching for an existing AMS subscription ID", func() {
			list, err := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{
				AmsSubscriptionIds: []string{FakeSubscriptionID.String()},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))

		})

		It("discarding AMS subscription ID field", func() {
			list, err := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(1))
		})

		It("searching for a non-existing AMS Subscription ID", func() {
			list, err := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{
				AmsSubscriptionIds: []string{"1h89fvtqeelulpo0fl5oddngj2ao7XXX"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list.GetPayload())).Should(Equal(0))
		})

		It("searching for both existing and non-existing AMS subscription IDs", func() {
			list, err := userBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{
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
		_, err = agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
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

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {

		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             sshPublicKey,
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
			registerHostsAndSetRolesDHCP(clusterID, 5)
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
			reply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			Expect(reply.GetPayload().OpenshiftClusterID).To(Equal(*strToUUID("41940ee8-ec99-43de-8766-174381b4921d")))
		})
	})

	It("moves between DHCP modes", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRolesDHCP(clusterID, 5)
		reply, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(swag.StringValue(reply.Payload.Status)).To(Equal(models.ClusterStatusPendingForInput))
		generateDhcpStepReply(reply.Payload.Hosts[0], "1.2.3.102", "1.2.3.103", true)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				APIVip:     swag.String("1.2.3.100"),
				IngressVip: swag.String("1.2.3.101"),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
		reply, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(swag.StringValue(reply.Payload.Status)).To(Equal(models.ClusterStatusReady))
		reply, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				VipDhcpAllocation:  swag.Bool(true),
				MachineNetworkCidr: swag.String("1.2.3.0/24"),
			},
			ClusterID: clusterID,
		})
		Expect(err).ToNot(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				APIVip:     swag.String("1.2.3.100"),
				IngressVip: swag.String("1.2.3.101"),
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
		generateDhcpStepReply(reply.Payload.Hosts[0], "1.2.3.102", "1.2.3.103", false)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)
		getReply, err := userBMClient.Installer.GetCluster(ctx, installer.NewGetClusterParams().WithClusterID(clusterID))
		Expect(err).ToNot(HaveOccurred())
		c := getReply.Payload
		Expect(swag.StringValue(c.Status)).To(Equal(models.ClusterStatusReady))
		Expect(c.APIVip).To(Equal("1.2.3.102"))
		Expect(c.IngressVip).To(Equal("1.2.3.103"))
	})
})

var _ = Describe("Validate BaseDNSDomain when creating a cluster", func() {
	var (
		ctx         = context.Background()
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)
	AfterEach(func() {
		clearDB()
	})
	type DNSTest struct {
		It            string
		BaseDNSDomain string
		ShouldThrow   bool
	}
	createClusterWithBaseDNS := func(baseDNS string) (*installer.RegisterClusterCreated, error) {
		return userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            baseDNS,
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             sshPublicKey,
			},
		})
	}
	tests := []DNSTest{
		{
			It:            "RegisterCluster should throw an error. BaseDNSDomain='example', not a valid DNS structure string",
			BaseDNSDomain: "example",
			ShouldThrow:   true,
		},
		{
			It:            "RegisterCluster should throw an error. BaseDNSDomain='example.c', Invalid top-level domain name ",
			BaseDNSDomain: "example.c",
			ShouldThrow:   true,
		},
		{
			It:            "RegisterCluster should throw an error. BaseDNSDomain='-example.com', Illegal character in domain name",
			BaseDNSDomain: "-example.com",
			ShouldThrow:   true,
		},
		{
			It:            "RegisterCluster should not throw an error. BaseDNSDomain='example.com', valid DNS",
			BaseDNSDomain: "example.com",
			ShouldThrow:   false,
		},
		{
			It:            "RegisterCluster should not throw an error. BaseDNSDomain='sub.example.com', valid DNS",
			BaseDNSDomain: "sub.example.com",
			ShouldThrow:   false,
		},
		{
			It:            "RegisterCluster should not throw an error. BaseDNSDomain='deep.sub.example.com', valid DNS",
			BaseDNSDomain: "deep.sub.example.com",
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

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		var registerClusterReply *installer.RegisterClusterCreated
		registerClusterReply, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             sshPublicKey,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		clusterID = *cluster.ID
		log.Infof("Register cluster %s", cluster.ID.String())
	})
	Context("Update BaseDNS", func() {
		It("Should not throw an error with valid 2 part DNS", func() {
			_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					BaseDNSDomain: swag.String("abc.com"),
				},
				ClusterID: clusterID,
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("Should not throw an error with valid 3 part DNS", func() {
			_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					BaseDNSDomain: swag.String("abc.def.com"),
				},
				ClusterID: clusterID,
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
	It("Should throw an error with invalid top-level domain", func() {
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				BaseDNSDomain: swag.String("abc.com.c"),
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
	})
	It("Should throw an error with invalid char prefix domain", func() {
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				BaseDNSDomain: swag.String("-abc.com"),
			},
			ClusterID: clusterID,
		})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("cluster install", func() {
	var (
		ctx         = context.Background()
		cluster     *models.Cluster
		clusterCIDR = "10.128.0.0/14"
		serviceCIDR = "172.30.0.0/16"
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:            "example.com",
				ClusterNetworkCidr:       &clusterCIDR,
				ClusterNetworkHostPrefix: 23,
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         swag.String(openshiftVersion),
				PullSecret:               swag.String(pullSecret),
				ServiceNetworkCidr:       &serviceCIDR,
				SSHPublicKey:             sshPublicKey,
				VipDhcpAllocation:        swag.Bool(true),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
		log.Infof("Register cluster %s", cluster.ID.String())
	})

	It("auto-assign", func() {
		By("register 3 hosts all with master hw information cluster expected to be ready")
		clusterID := *cluster.ID
		hosts := register3nodes(ctx, clusterID)
		h1, h2, h3 := hosts[0], hosts[1], hosts[2]
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("change first host hw info to worker and expect the cluster to become insufficient")
		generateHWPostStepReply(ctx, h1, validWorkerHwInfo, "h1")
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("add two more hosts with master inventory expect the cluster to be ready")
		h4 := registerNode(ctx, clusterID, "h4")
		h5 := &registerHost(clusterID).Host

		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)
		generateEssentialHostSteps(ctx, h5, "h5")

		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4, h5)
		waitForHostState(ctx, clusterID, *h4.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
		waitForHostState(ctx, clusterID, *h5.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("add hosts with worker inventory expect the cluster to be ready")
		h6 := &registerHost(clusterID).Host
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		generateEssentialHostStepsWithInventory(ctx, h6, "h6", validWorkerHwInfo)
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4, h5, h6)
		waitForHostState(ctx, clusterID, *h6.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)

		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
			IgnoreStateInfo)

		By("start installation and validate roles")
		_, err := userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		generateEssentialPrepareForInstallationSteps(ctx, h1, h2, h3, h4, h5, h6)
		waitForClusterState(context.Background(), clusterID, models.ClusterStatusInstalling,
			3*time.Minute, IgnoreStateInfo)
		getHostRole := func(id strfmt.UUID) models.HostRole {
			var reply *installer.GetHostOK
			reply, err = userBMClient.Installer.GetHost(ctx, &installer.GetHostParams{
				ClusterID: clusterID,
				HostID:    id,
			})
			Expect(err).ShouldNot(HaveOccurred())
			return reply.GetPayload().Role
		}
		Expect(getHostRole(*h1.ID)).Should(Equal(models.HostRoleWorker))
		Expect(getHostRole(*h6.ID)).Should(Equal(models.HostRoleWorker))
		getReply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
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
	})

	Context("usage", func() {
		It("report usage on default features with SNO", func() {
			registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:            "example.com",
					ClusterNetworkCidr:       &clusterCIDR,
					ClusterNetworkHostPrefix: 23,
					Name:                     swag.String("sno-cluster"),
					OpenshiftVersion:         swag.String(snoVersion),
					PullSecret:               swag.String(pullSecret),
					ServiceNetworkCidr:       &serviceCIDR,
					SSHPublicKey:             sshPublicKey,
					VipDhcpAllocation:        swag.Bool(true),
					HighAvailabilityMode:     swag.String(models.ClusterHighAvailabilityModeNone),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster = registerClusterReply.GetPayload()
			getReply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: *cluster.ID})
			Expect(err).NotTo(HaveOccurred())
			log.Infof("usage after create: %s\n", getReply.Payload.FeatureUsage)
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.HighAvailabilityModeUsage})
			verifyUsageNotSet(getReply.Payload.FeatureUsage, strings.ToUpper("console"), usage.VipDhcpAllocationUsage)
		})

		It("report usage on update cluster", func() {
			clusterID := *cluster.ID
			h := &registerHost(clusterID).Host
			ntpSources := "1.1.1.1,2.2.2.2"
			proxy := "http://1.1.1.1:8080"
			no_proxy := "a.redhat.com"
			_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					VipDhcpAllocation:   swag.Bool(true),
					AdditionalNtpSource: &ntpSources,
					HTTPProxy:           &proxy,
					HTTPSProxy:          &proxy,
					NoProxy:             &no_proxy,
					HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
						{ID: *h.ID, Hostname: "h1"},
					},
				},
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			getReply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			verifyUsageSet(getReply.Payload.FeatureUsage,
				models.Usage{Name: usage.VipDhcpAllocationUsage},
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

	Context("MachineNetworkCIDR auto assign", func() {

		It("MachineNetworkCIDR successful allocating", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			_ = registerNode(ctx, clusterID, "test-host")
			c, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip:            &apiVip,
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(c.GetPayload().Hosts)).Should(Equal(1))
			Expect(c.Payload.APIVip).Should(Equal(apiVip))
			Expect(c.Payload.MachineNetworkCidr).Should(Equal("1.2.3.0/24"))
		})

		It("MachineNetworkCIDR successful deallocating ", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			host := registerNode(ctx, clusterID, "test-host")
			c, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip:            &apiVip,
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Payload.APIVip).Should(Equal(apiVip))
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).ShouldNot(HaveOccurred())
			_, err1 := userBMClient.Installer.DeregisterHost(ctx, &installer.DeregisterHostParams{
				ClusterID: clusterID,
				HostID:    *host.ID,
			})
			Expect(err1).ShouldNot(HaveOccurred())
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "", defaultWaitForMachineNetworkCIDRTimeout)).ShouldNot(HaveOccurred())
		})

		It("MachineNetworkCIDR no vips - no allocation", func() {
			clusterID := *cluster.ID
			c, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(c.GetPayload().Hosts)).Should(Equal(0))
			Expect(c.Payload.APIVip).Should(Equal(""))
			Expect(c.Payload.IngressVip).Should(Equal(""))
			Expect(c.Payload.MachineNetworkCidr).Should(Equal(""))
			_ = registerNode(ctx, clusterID, "test-host")
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).Should(HaveOccurred())
			c1, err1 := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err1).NotTo(HaveOccurred())
			Expect(c1.Payload.MachineNetworkCidr).Should(Equal(""))
		})

		It("MachineNetworkCIDR no hosts - no allocation", func() {
			clusterID := *cluster.ID
			apiVip := "1.2.3.8"
			_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip:            &apiVip,
					VipDhcpAllocation: swag.Bool(false),
				},
				ClusterID: clusterID,
			})
			Expect(err).To(HaveOccurred())
			Expect(waitForMachineNetworkCIDR(
				ctx, clusterID, "1.2.3.0/24", defaultWaitForMachineNetworkCIDRTimeout)).Should(HaveOccurred())
			c1, err1 := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err1).NotTo(HaveOccurred())
			Expect(c1.Payload.MachineNetworkCidr).Should(Equal(""))
		})
	})

	Context("install cluster cases", func() {
		var clusterID strfmt.UUID
		BeforeEach(func() {
			clusterID = *cluster.ID
			registerHostsAndSetRoles(clusterID, 5)
		})

		Context("NTP cases", func() {
			It("Update NTP source", func() {
				c, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				hosts := c.GetPayload().Hosts

				By("Verify NTP step", func() {
					step, ok := getStepInList(getNextSteps(clusterID, *hosts[0].ID), models.StepTypeNtpSynchronizer)
					Expect(ok).Should(Equal(true))

					requestStr := step.Args[len(step.Args)-1]
					var ntpRequest models.NtpSynchronizationRequest

					Expect(json.Unmarshal([]byte(requestStr), &ntpRequest)).ShouldNot(HaveOccurred())
					Expect(*ntpRequest.NtpSource).Should(Equal(c.Payload.AdditionalNtpSource))
				})

				By("Update NTP source", func() {
					newSource := "5.5.5.5"

					reply, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &newSource,
						},
						ClusterID: clusterID,
					})
					Expect(err).ShouldNot(HaveOccurred())
					Expect(reply.Payload.AdditionalNtpSource).Should(Equal(newSource))

					step, ok := getStepInList(getNextSteps(clusterID, *hosts[0].ID), models.StepTypeNtpSynchronizer)
					Expect(ok).Should(Equal(true))

					requestStr := step.Args[len(step.Args)-1]
					var ntpRequest models.NtpSynchronizationRequest

					Expect(json.Unmarshal([]byte(requestStr), &ntpRequest)).ShouldNot(HaveOccurred())
					Expect(*ntpRequest.NtpSource).Should(Equal(newSource))
				})
			})

			It("Unsynced host", func() {
				Skip("IsNTPSynced isn't mandatory validation for host isSufficientForInstall")

				c, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				hosts := c.GetPayload().Hosts

				By("unsync", func() {
					generateNTPPostStepReply(ctx, hosts[0], []*models.NtpSource{
						{SourceName: common.TestNTPSourceSynced.SourceName, SourceState: models.SourceStateUnreachable},
					})
					waitForHostState(ctx, clusterID, *hosts[0].ID, models.HostStatusInsufficient, defaultWaitForHostStateTimeout)
				})

				By("Set new NTP source", func() {
					newSource := "5.5.5.5"

					_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
						ClusterUpdateParams: &models.ClusterUpdateParams{
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

				waitForHostState(ctx, clusterID, *hosts[0].ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
			})
		})

		It("disable enable master", func() {
			By("get masters")
			c, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			hosts := getClusterMasters(c.GetPayload())
			Expect(len(hosts)).Should(Equal(3))

			By("disable master, expect cluster to become insufficient")
			disableRet, err := userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
				HostID:    *hosts[0].ID,
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(disableRet.GetPayload().Status)).Should(Equal(models.ClusterStatusInsufficient))

			By("enable master")
			_, err = userBMClient.Installer.EnableHost(ctx, &installer.EnableHostParams{
				HostID:    *hosts[0].ID,
				ClusterID: clusterID,
			})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("register host while installing", func() {
			installCluster(clusterID)
			waitForClusterState(ctx, clusterID, models.ClusterStatusInstalling, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
			_, err := agentBMClient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
				ClusterID: clusterID,
				NewHostParams: &models.HostCreateParams{
					HostID: strToUUID(uuid.New().String()),
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewRegisterHostConflict()))
		})

		It("register host while cluster in error state", func() {
			FailCluster(ctx, clusterID, masterFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
			_, err := agentBMClient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
				ClusterID: clusterID,
				NewHostParams: &models.HostCreateParams{
					HostID: strToUUID(uuid.New().String()),
				},
			})
			Expect(err).To(BeAssignableToTypeOf(installer.NewRegisterHostConflict()))
		})

		It("fail installation if there is only a single worker that manages to install", func() {
			FailCluster(ctx, clusterID, workerFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)
		})

		It("register existing host while cluster in installing state", func() {
			c := installCluster(clusterID)
			hostID := c.Hosts[0].ID
			_, err := agentBMClient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
				ClusterID: clusterID,
				NewHostParams: &models.HostCreateParams{
					HostID: hostID,
				},
			})
			Expect(err).To(BeNil())
			host := getHost(clusterID, *hostID)
			Expect(*host.Status).To(Equal("error"))
		})

		It("register host after reboot - wrong boot order", func() {
			c := installCluster(clusterID)
			hostID := c.Hosts[0].ID

			_, ok := getStepInList(getNextSteps(clusterID, *hostID), models.StepTypeInstall)
			Expect(ok).Should(Equal(true))

			installProgress := models.HostStageRebooting
			updateProgress(*hostID, clusterID, installProgress)

			By("Verify the db has been updated", func() {
				hostInDb := getHost(clusterID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostInDb.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostInDb.InstallationDiskID).ShouldNot(BeEmpty())
				Expect(hostInDb.InstallationDiskPath).ShouldNot(BeEmpty())
				Expect(hostInDb.Inventory).ShouldNot(BeEmpty())
			})

			By("Try to register", func() {
				_, err := agentBMClient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
					ClusterID: clusterID,
					NewHostParams: &models.HostCreateParams{
						HostID: hostID,
					},
				})
				Expect(err).To(BeNil())
				hostInDb := getHost(clusterID, *hostID)
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
				updateProgress(*hostID, clusterID, installProgress)
			})

			By("Verify the db has been updated", func() {
				hostInDb := getHost(clusterID, *hostID)
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

		It("install_cluster fail", func() {
			By("Installing cluster till finalize")
			setClusterAsFinalizing(ctx, clusterID)
			By("Failing installation")
			completeInstallationAndVerify(context.Background(), agentBMClient, clusterID, false)

			By("Verifying completion date field")
			resp, _ := agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(resp.GetPayload().InstallCompletedAt).Should(Equal(resp.GetPayload().StatusUpdatedAt))
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
			_, err := agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
				ClusterID: clusterID,
				HostID:    masterID,
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
			_, err := agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
				ClusterID: clusterID,
				HostID:    *c.Hosts[0].ID,
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
			_, status, _ := isHostInState(ctx, clusterID, *c.Hosts[0].ID, models.HostStatusInstalling)
			Expect(status).Should(Equal(models.HostStatusInstalling))

		})

		// TODO: re-enable the test when cluster monitor state will be affected by hosts states and cluster
		// will not be ready of all the hosts are not ready.
		//It("installation_conflicts", func() {
		//	By("try to install host with host without a role")
		//	host := &registerHost(clusterID).Host
		//	generateHWPostStepReply(host, validHwInfo, "host")
		//	_, err := userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		//	Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewInstallClusterConflict())))
		//	By("install after disabling host without a role")
		//	_, err = userBMClient.Installer.DisableHost(ctx,
		//		&installer.DisableHostParams{ClusterID: clusterID, HostID: *host.ID})
		//	Expect(err).NotTo(HaveOccurred())
		//	_, err = userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		//	Expect(err).NotTo(HaveOccurred())
		//})

		Context("report logs progress", func() {
			verifyLogProgress := func(c *models.Cluster, host_progress models.LogsState, cluster_progress models.LogsState) {
				Expect(c.ControllerLogsStartedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
				Expect(c.LogsInfo).To(Equal(cluster_progress))
				for _, host := range c.Hosts {
					Expect(host.LogsStartedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
					Expect(host.LogsInfo).To(Equal(host_progress))
				}
			}
			It("log progress installation succeed", func() {
				By("report log progress by host and cluster during installation")
				c := installCluster(clusterID)
				requested := models.LogsStateRequested
				completed := models.LogsStateCompleted
				for _, host := range c.Hosts {
					updateHostLogProgress(clusterID, *host.ID, requested)
				}
				updateClusterLogProgress(clusterID, requested)

				c = getCluster(clusterID)
				verifyLogProgress(c, requested, requested)

				By("report log progress by cluster during finalizing")
				for _, host := range c.Hosts {
					updateHostLogProgress(clusterID, *host.ID, completed)
					updateProgress(*host.ID, clusterID, models.HostStageDone)
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

				_, err := agentBMClient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: installProgress,
					HostID:       *hosts[0].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			// Host #1

			By("progress_to_other_host", func() {
				installProgress := models.HostStageWritingImageToDisk
				installInfo := "68%"
				updateProgressWithInfo(*hosts[0].ID, clusterID, installProgress, installInfo)
				hostFromDB := getHost(clusterID, *hosts[0].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(installInfo))
			})

			By("report_done", func() {
				installProgress := models.HostStageDone
				updateProgress(*hosts[0].ID, clusterID, installProgress)
				hostFromDB := getHost(clusterID, *hosts[0].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstalled))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("cant_report_after_done", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageFailed,
				}

				_, err := agentBMClient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: installProgress,
					HostID:       *hosts[0].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			// Host #2

			By("progress_to_some_host", func() {
				installProgress := models.HostStageWritingImageToDisk
				updateProgress(*hosts[1].ID, clusterID, installProgress)
				hostFromDB := getHost(clusterID, *hosts[1].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(installProgress)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(installProgress))
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("invalid_lower_stage", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageInstalling,
				}

				_, err := agentBMClient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: installProgress,
					HostID:       *hosts[1].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			By("report_failed_on_same_host", func() {
				installProgress := models.HostStageFailed
				installInfo := "because some error"
				updateProgressWithInfo(*hosts[1].ID, clusterID, installProgress, installInfo)
				hostFromDB := getHost(clusterID, *hosts[1].ID)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", installProgress, installInfo)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk)) // Last stage
				Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
			})

			By("cant_report_after_error", func() {
				installProgress := &models.HostProgress{
					CurrentStage: models.HostStageDone,
				}

				_, err := agentBMClient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: installProgress,
					HostID:       *hosts[1].ID,
				})

				Expect(err).Should(HaveOccurred())
			})

			By("verify_everything_changed_error", func() {
				waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
					IgnoreStateInfo)
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusError, defaultWaitForHostStateTimeout)
				}
			})
		})

		It("[minimal-set]install download_config_files", func() {
			//Test downloading kubeconfig files in worng state
			//This test uses Agent Auth for DownloadClusterFiles (as opposed to the other tests), to cover both supported authentication types for this API endpoint.
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())

			defer os.Remove(file.Name())
			_, err = agentBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewDownloadClusterFilesConflict())))

			installCluster(clusterID)

			missingClusterId := strfmt.UUID(uuid.New().String())
			_, err = agentBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: missingClusterId, FileName: "bootstrap.ign"}, file)
			Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterFilesNotFound())))

			_, err = agentBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "not_real_file"}, file)
			Expect(err).Should(HaveOccurred())

			_, err = agentBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("download_config_files in error state", func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(file.Name())

			FailCluster(ctx, clusterID, masterFailure)
			//Wait for cluster to get to error state
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)

			_, err = userBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "bootstrap.ign"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))

			By("Download install-config.yaml")
			_, err = userBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "install-config.yaml"}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err = file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
		})

		It("Get credentials", func() {
			By("Test getting credentials for not found cluster")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := userBMClient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: missingClusterId})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewGetCredentialsNotFound())))
			}
			By("Test getting credentials before console operator is available")
			{
				_, err := userBMClient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
				Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewGetCredentialsConflict())))
			}
			By("Test happy flow")
			{
				setClusterAsFinalizing(ctx, clusterID)
				completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
				creds, err := userBMClient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(creds.GetPayload().Username).To(Equal(bminventory.DefaultUser))
				Expect(creds.GetPayload().ConsoleURL).To(Equal(common.GetConsoleUrl(cluster.Name, cluster.BaseDNSDomain)))
				Expect(len(creds.GetPayload().Password)).NotTo(Equal(0))
			}
		})

		It("Upload and Download logs", func() {
			By("Download before upload")
			{

				nodes := register3nodes(ctx, clusterID)
				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterLogs(ctx, &installer.DownloadClusterLogsParams{ClusterID: clusterID, HostID: nodes[1].ID}, file)
				Expect(err).To(HaveOccurred())

			}

			By("Test happy flow small file")
			{
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				_ = register3nodes(ctx, clusterID)
				_, err = agentBMClient.Installer.UploadLogs(ctx, &installer.UploadLogsParams{ClusterID: clusterID, LogsType: string(models.LogsTypeController), Upfile: kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())
				logsType := string(models.LogsTypeController)
				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterLogs(ctx, &installer.DownloadClusterLogsParams{ClusterID: clusterID, LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
			}

			By("Test happy flow host logs file")
			{
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				hosts := register3nodes(ctx, clusterID)
				_, err = agentBMClient.Installer.UploadHostLogs(ctx, &installer.UploadHostLogsParams{ClusterID: clusterID, HostID: *hosts[0].ID, Upfile: kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())

				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadHostLogs(ctx, &installer.DownloadHostLogsParams{ClusterID: clusterID, HostID: *hosts[0].ID}, file)
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
				nodes := register3nodes(ctx, clusterID)
				// test hosts logs
				kubeconfigFile, err := os.Open(filePath)
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.UploadLogs(ctx, &installer.UploadLogsParams{ClusterID: clusterID, HostID: nodes[1].ID,
					Upfile: kubeconfigFile, LogsType: string(models.LogsTypeHost)})
				Expect(err).NotTo(HaveOccurred())
				h := getHost(clusterID, *nodes[1].ID)
				Expect(h.LogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
				logsType := string(models.LogsTypeHost)
				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterLogs(ctx, &installer.DownloadClusterLogsParams{ClusterID: clusterID,
					HostID: nodes[1].ID, LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
				// test controller logs
				kubeconfigFile, err = os.Open(filePath)
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.UploadLogs(ctx, &installer.UploadLogsParams{ClusterID: clusterID,
					Upfile: kubeconfigFile, LogsType: string(models.LogsTypeController)})
				Expect(err).NotTo(HaveOccurred())
				c := getCluster(clusterID)
				Expect(c.ControllerLogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
				logsType = string(models.LogsTypeController)
				file, err = ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterLogs(ctx, &installer.DownloadClusterLogsParams{ClusterID: clusterID,
					LogsType: &logsType}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err = file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
			}
		})

		It("Download cluster logs", func() {
			nodes := register3nodes(ctx, clusterID)
			for _, host := range nodes {
				kubeconfigFile, err := os.Open("test_kubeconfig")
				Expect(err).NotTo(HaveOccurred())
				_, err = agentBMClient.Installer.UploadLogs(ctx, &installer.UploadLogsParams{ClusterID: clusterID,
					HostID: host.ID, LogsType: string(models.LogsTypeHost), Upfile: kubeconfigFile})
				Expect(err).NotTo(HaveOccurred())
				kubeconfigFile.Close()
			}
			kubeconfigFile, err := os.Open("test_kubeconfig")
			Expect(err).NotTo(HaveOccurred())
			_, err = agentBMClient.Installer.UploadLogs(ctx, &installer.UploadLogsParams{ClusterID: clusterID,
				LogsType: string(models.LogsTypeController), Upfile: kubeconfigFile})
			Expect(err).NotTo(HaveOccurred())
			kubeconfigFile.Close()

			filePath := "../build/test_logs.tar"
			file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			Expect(err).NotTo(HaveOccurred())
			defer file.Close()
			logsType := string(models.LogsTypeAll)
			_, err = userBMClient.Installer.DownloadClusterLogs(ctx, &installer.DownloadClusterLogsParams{ClusterID: clusterID, LogsType: &logsType}, file)
			Expect(err).NotTo(HaveOccurred())
			s, err := file.Stat()
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Size()).ShouldNot(Equal(0))
			file.Close()
			file, err = os.Open(filePath)
			Expect(err).NotTo(HaveOccurred())
			tarReader := tar.NewReader(file)
			numOfarchivedFiles := 0
			for {
				_, err := tarReader.Next()
				if err == io.EOF {
					break
				}
				Expect(err).NotTo(HaveOccurred())
				numOfarchivedFiles += 1
				Expect(numOfarchivedFiles <= len(nodes)+1).Should(Equal(true))
			}
			Expect(numOfarchivedFiles).Should(Equal(len(nodes) + 1))

		})

		It("Upload ingress ca and kubeconfig download", func() {

			By("Upload ingress ca for not existent clusterid")
			{
				missingClusterId := strfmt.UUID(uuid.New().String())
				_, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: missingClusterId, IngressCertParams: "dummy"})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertNotFound())))
			}
			By("Test getting upload ingress ca in wrong state")
			{
				_, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: "dummy"})
				Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertBadRequest())))
			}
			By("Test happy flow")
			{
				setClusterAsFinalizing(ctx, clusterID)
				// Download kubeconfig before uploading
				kubeconfigNoIngress, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: "kubeconfig-noingress"}, kubeconfigNoIngress)
				Expect(err).NotTo(HaveOccurred())
				sni, err := kubeconfigNoIngress.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(sni.Size()).ShouldNot(Equal(0))

				By("Trying to download kubeconfig file before it exists")
				file, err := ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterKubeconfig(ctx, &installer.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
				Expect(err).Should(HaveOccurred())
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterKubeconfigConflict())))

				By("Upload ingress ca")
				res, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(reflect.TypeOf(res)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))

				// Download kubeconfig after uploading
				file, err = ioutil.TempFile("", "tmp")
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.DownloadClusterKubeconfig(ctx, &installer.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
				Expect(err).NotTo(HaveOccurred())
				s, err := file.Stat()
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Size()).ShouldNot(Equal(0))
				Expect(s.Size()).ShouldNot(Equal(sni.Size()))
			}
			By("Try to upload ingress ca second time, do nothing and return ok")
			{
				// Try to upload ingress ca second time
				res, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
				Expect(err).NotTo(HaveOccurred())
				Expect(reflect.TypeOf(res)).To(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))
			}
		})

		It("on cluster error - verify all hosts are aborted", func() {
			FailCluster(ctx, clusterID, masterFailure)
			waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout, clusterErrorInfo)
			rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			for _, host := range c.Hosts {
				waitForHostState(ctx, clusterID, *host.ID, models.HostStatusError, defaultWaitForHostStateTimeout)
			}
		})

		Context("cancel installation", func() {
			It("cancel running installation", func() {
				c := installCluster(clusterID)
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling,
						defaultWaitForHostStateTimeout)
				}
				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForClusterState(ctx, clusterID, models.ClusterStatusCancelled, defaultWaitForClusterStateTimeout, clusterCanceledInfo)
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
				for _, host := range c.Hosts {
					Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusCancelled))
				}

				Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
			})
			It("cancel installation conflicts", func() {
				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewCancelInstallationConflict())))
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusReady))
			})
			It("cancel failed cluster", func() {
				By("verify cluster is in error")
				FailCluster(ctx, clusterID, masterFailure)
				waitForClusterState(ctx, clusterID, models.ClusterStatusError, defaultWaitForClusterStateTimeout,
					clusterErrorInfo)
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusError))
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusError,
						defaultWaitForHostStateTimeout)
				}
				By("cancel installation, check cluster and hosts statuses")
				_, err = userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				rep, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
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

				updateProgress(*c.Hosts[0].ID, clusterID, "Installing")
				updateProgress(*c.Hosts[1].ID, clusterID, "Done")

				h1 := getHost(clusterID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHost(clusterID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusCancelled,
						defaultWaitForClusterStateTimeout)
				}
			})
			It("cancel installation with a disabled host", func() {
				By("register a new worker")
				disabledHost := registerNode(ctx, clusterID, "hostname")
				_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
						{ID: *disabledHost.ID, Role: models.HostRoleUpdateParamsWorker},
					},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())

				By("disable worker")
				_, err = userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
					ClusterID: clusterID,
					HostID:    *disabledHost.ID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				waitForHostState(ctx, clusterID, *disabledHost.ID, models.HostStatusDisabled,
					defaultWaitForHostStateTimeout)
				waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
					clusterReadyStateInfo)

				By("install cluster")
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(6))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling,
						defaultWaitForHostStateTimeout)
				}

				By("cancel installation")
				_, err = userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(len(c.Hosts)).Should(Equal(6))
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusCancelled))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusCancelled))
				}
			})
			It("cancel host - wrong boot order", func() {
				c := installCluster(clusterID)
				hostID := c.Hosts[0].ID
				_, ok := getStepInList(getNextSteps(clusterID, *hostID), models.StepTypeInstall)
				Expect(ok).Should(Equal(true))
				updateProgress(*hostID, clusterID, models.HostStageRebooting)

				_, err := agentBMClient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
					ClusterID: clusterID,
					NewHostParams: &models.HostCreateParams{
						HostID: hostID,
					},
				})
				Expect(err).ShouldNot(HaveOccurred())
				hostInDb := getHost(clusterID, *hostID)
				Expect(*hostInDb.Status).Should(Equal(models.HostStatusInstallingPendingUserAction))

				waitForClusterState(
					ctx,
					clusterID,
					models.ClusterStatusInstallingPendingUserAction,
					defaultWaitForClusterStateTimeout,
					clusterInstallingPendingUserActionStateInfo)

				_, err = userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusCancelled,
						defaultWaitForHostStateTimeout)
				}
			})
			It("cancel installation - cluster in finalizing status", func() {
				setClusterAsFinalizing(ctx, clusterID)
				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())

				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(c).NotTo(BeNil())

				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusCancelled,
						defaultWaitForHostStateTimeout)
				}
			})
		})
		Context("reset installation", func() {
			enableReset, _ := strconv.ParseBool(os.Getenv("ENABLE_RESET"))

			It("reset cluster and register hosts", func() {
				By("verify reset success")
				installCluster(clusterID)
				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())

				By("verify cluster state")
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))

				By("verify hosts state")
				for i, host := range c.Hosts {
					if enableReset {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
						_, ok := getStepInList(getNextSteps(clusterID, *host.ID), models.StepTypeResetInstallation)
						Expect(ok).Should(Equal(true))
					} else {
						waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
							defaultWaitForHostStateTimeout)
					}
					_, err = agentBMClient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
						ClusterID: clusterID,
						NewHostParams: &models.HostCreateParams{
							HostID: host.ID,
						},
					})
					Expect(err).ShouldNot(HaveOccurred())
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusDiscovering,
						defaultWaitForHostStateTimeout)
					generateEssentialHostSteps(ctx, host, fmt.Sprintf("host-after-reset-%d", i))
				}
				generateFullMeshConnectivity(ctx, "1.2.3.10", c.Hosts...)
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusKnown,
						defaultWaitForHostStateTimeout)
					host = getHost(clusterID, *host.ID)
					Expect(host.Progress.CurrentStage).Should(Equal(models.HostStage("")))
					Expect(host.Progress.ProgressInfo).Should(Equal(""))
					Expect(host.Bootstrap).Should(Equal(false))
				}
			})
			It("reset cluster and disable bootstrap", func() {
				if enableReset {
					var bootstrapID *strfmt.UUID

					By("verify reset success")
					installCluster(clusterID)
					_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
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
					rep, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
					Expect(err).NotTo(HaveOccurred())
					c = rep.GetPayload()
					Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))

					By("register hosts and disable bootstrap")
					for i, host := range c.Hosts {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
						_, ok := getStepInList(getNextSteps(clusterID, *host.ID), models.StepTypeResetInstallation)
						Expect(ok).Should(Equal(true))
						_, err = agentBMClient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
							ClusterID: clusterID,
							NewHostParams: &models.HostCreateParams{
								HostID: host.ID,
							},
						})
						Expect(err).ShouldNot(HaveOccurred())
						waitForHostState(ctx, clusterID, *host.ID, models.HostStatusDiscovering,
							defaultWaitForHostStateTimeout)
						generateEssentialHostSteps(ctx, host, fmt.Sprintf("host-after-reset-%d", i))
					}
					generateFullMeshConnectivity(ctx, "1.2.3.10", c.Hosts...)
					for _, host := range c.Hosts {
						waitForHostState(ctx, clusterID, *host.ID, models.HostStatusKnown,
							defaultWaitForHostStateTimeout)

						if host.Bootstrap {
							_, err = userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
								ClusterID: clusterID,
								HostID:    *host.ID,
							})
							Expect(err).NotTo(HaveOccurred())
						}
					}
					h := registerNode(ctx, clusterID, "hostname")
					_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
						ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
							{ID: *h.ID, Role: models.HostRoleUpdateParamsMaster},
						},
						},
						ClusterID: clusterID,
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
				_, err := userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewResetClusterConflict())))
				c := installCluster(clusterID)
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling,
						defaultWaitForHostStateTimeout)
				}
				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				for _, host := range c.Hosts {
					if enableReset {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
					} else {
						waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
							defaultWaitForHostStateTimeout)
					}
				}
			})
			It("reset cluster with various hosts states", func() {
				c := installCluster(clusterID)
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalling))
				Expect(len(c.Hosts)).Should(Equal(5))

				updateProgress(*c.Hosts[0].ID, clusterID, "Installing")
				updateProgress(*c.Hosts[1].ID, clusterID, "Done")

				h1 := getHost(clusterID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHost(clusterID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err := userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
						defaultWaitForClusterStateTimeout)
				}
			})

			It("reset cluster - wrong boot order", func() {
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(5))
				updateProgress(*c.Hosts[0].ID, clusterID, models.HostStageRebooting)
				_, err := userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterResetStateInfo)
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
						defaultWaitForHostStateTimeout)
					_, err = agentBMClient.Installer.RegisterHost(ctx, &installer.RegisterHostParams{
						ClusterID: clusterID,
						NewHostParams: &models.HostCreateParams{
							HostID: host.ID,
						},
					})
					Expect(err).ShouldNot(HaveOccurred())
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusDiscovering,
						defaultWaitForHostStateTimeout)
				}
			})
			It("reset cluster with a disabled host", func() {
				By("register a new worker")
				disabledHost := registerNode(ctx, clusterID, "hostname")
				_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
						{ID: *disabledHost.ID, Role: models.HostRoleUpdateParamsWorker},
					},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())

				By("disable worker")
				_, err = userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
					ClusterID: clusterID,
					HostID:    *disabledHost.ID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				waitForHostState(ctx, clusterID, *disabledHost.ID, models.HostStatusDisabled,
					defaultWaitForHostStateTimeout)
				waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
					clusterReadyStateInfo)

				By("install cluster")
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(6))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling,
						defaultWaitForHostStateTimeout)
				}

				By("reset installation")
				_, err = userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(len(c.Hosts)).Should(Equal(6))
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					if enableReset {
						Expect(swag.StringValue(host.Status)).Should(Equal(models.HostStatusResetting))
					} else {
						waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
							defaultWaitForHostStateTimeout)
					}
				}
			})

			It("reset cluster with hosts after reboot and one disabled host", func() {
				By("register a new worker")
				disabledHost := registerNode(ctx, clusterID, "hostname")
				_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
						{ID: *disabledHost.ID, Role: models.HostRoleUpdateParamsWorker},
					},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())

				By("disable worker")
				_, err = userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
					ClusterID: clusterID,
					HostID:    *disabledHost.ID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				waitForHostState(ctx, clusterID, *disabledHost.ID, models.HostStatusDisabled,
					defaultWaitForHostStateTimeout)
				waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
					clusterReadyStateInfo)

				By("install cluster")
				c := installCluster(clusterID)
				Expect(len(c.Hosts)).Should(Equal(6))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusInstalling,
						defaultWaitForHostStateTimeout)
					updateProgress(*host.ID, clusterID, models.HostStageRebooting)
				}

				By("reset installation and verify hosts statuses")
				_, err = userBMClient.Installer.CancelInstallation(ctx, &installer.CancelInstallationParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c = rep.GetPayload()
				Expect(len(c.Hosts)).Should(Equal(6))
				Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
				for _, host := range c.Hosts {
					if host.ID.String() == disabledHost.ID.String() {
						Expect(*host.Status).Should(Equal(models.HostStatusDisabled))
						continue
					}
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
						defaultWaitForClusterStateTimeout)
				}
			})

			It("reset installation - cluster in finalizing status", func() {
				setClusterAsFinalizing(ctx, clusterID)
				_, err := userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).ShouldNot(HaveOccurred())

				rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := rep.GetPayload()
				Expect(c).NotTo(BeNil())

				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
						defaultWaitForHostStateTimeout)
				}
			})

			It("reset cluster doesn't delete manifests", func() {
				content := `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 99-openshift-machineconfig-master-kargs
spec:
  kernelArguments:
  - 'loglevel=7'`
				base64Content := base64.StdEncoding.EncodeToString([]byte(content))
				manifest := models.Manifest{
					FileName: "99-openshift-machineconfig-master-kargs.yaml",
					Folder:   "openshift",
				}
				response, err := userBMClient.Manifests.CreateClusterManifest(ctx, &manifests.CreateClusterManifestParams{
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

				updateProgress(*c.Hosts[0].ID, clusterID, "Installing")
				updateProgress(*c.Hosts[1].ID, clusterID, "Done")

				h1 := getHost(clusterID, *c.Hosts[0].ID)
				Expect(*h1.Status).Should(Equal(models.HostStatusInstallingInProgress))
				h2 := getHost(clusterID, *c.Hosts[1].ID)
				Expect(*h2.Status).Should(Equal(models.HostStatusInstalled))

				_, err = userBMClient.Installer.ResetCluster(ctx, &installer.ResetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				for _, host := range c.Hosts {
					waitForHostState(ctx, clusterID, *host.ID, models.HostStatusResettingPendingUserAction,
						defaultWaitForClusterStateTimeout)
				}

				// verify manifest remains after cluster reset
				response2, err := userBMClient.Manifests.ListClusterManifests(ctx, &manifests.ListClusterManifestsParams{
					ClusterID: *cluster.ID,
				})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(response2.Payload).Should(ContainElement(&manifest))
			})

			AfterEach(func() {
				reply, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(reply.GetPayload().OpenshiftClusterID).To(Equal(*strToUUID("")))
			})
		})
	})

	It("install cluster requirement", func() {
		clusterID := *cluster.ID
		waitForClusterState(ctx, clusterID, models.ClusterStatusPendingForInput, defaultWaitForClusterStateTimeout,
			clusterPendingForInputStateInfo)

		checkUpdateAtWhileStatic(ctx, clusterID)

		hosts := register3nodes(ctx, clusterID)
		h4 := &registerHost(clusterID).Host
		h5 := registerNode(ctx, clusterID, "h5")

		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"

		By("Two hosts are masters, one host is without role  -> state must be insufficient")
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *hosts[0].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *hosts[1].ID, Role: models.HostRoleUpdateParamsMaster},
			},
				APIVip:     &apiVip,
				IngressVip: &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		// add host and 2 workers (h4 has no inventory) --> insufficient state due to single worker
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *hosts[2].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h4.ID, Role: models.HostRoleUpdateParamsWorker},
				{ID: *h5.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		// update host4 again (now it has inventory) -> state must be ready
		generateEssentialHostSteps(ctx, h4, "h4")
		// update role for the host4 to master -> state must be ready
		generateFullMeshConnectivity(ctx, "1.2.3.10", hosts[0], hosts[1], hosts[2], h4, h5)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h4.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)
	})

	It("install_cluster_states", func() {
		clusterID := *cluster.ID
		waitForClusterState(ctx, clusterID, models.ClusterStatusPendingForInput, 60*time.Second, clusterPendingForInputStateInfo)

		wh1 := registerNode(ctx, clusterID, "wh1")
		wh2 := registerNode(ctx, clusterID, "wh2")
		wh3 := registerNode(ctx, clusterID, "wh3")
		generateFullMeshConnectivity(ctx, "1.2.3.10", wh1, wh2, wh3)

		apiVip := "1.2.3.5"
		ingressVip := "1.2.3.6"

		By("All hosts are workers -> state must be insufficient")
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *wh1.ID, Role: models.HostRoleUpdateParamsWorker},
				{ID: *wh2.ID, Role: models.HostRoleUpdateParamsWorker},
				{ID: *wh3.ID, Role: models.HostRoleUpdateParamsWorker},
			},
				VipDhcpAllocation: swag.Bool(false),
				APIVip:            &apiVip,
				IngressVip:        &ingressVip,
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)
		clusterReply, getErr := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{
			ClusterID: clusterID,
		})
		Expect(getErr).ToNot(HaveOccurred())

		Expect(clusterReply.Payload.APIVip).To(Equal(apiVip))
		Expect(clusterReply.Payload.MachineNetworkCidr).To(Equal("1.2.3.0/24"))
		Expect(len(clusterReply.Payload.HostNetworks)).To(Equal(1))
		Expect(clusterReply.Payload.HostNetworks[0].Cidr).To(Equal("1.2.3.0/24"))

		mh1 := registerNode(ctx, clusterID, "mh1")
		generateFAPostStepReply(ctx, mh1, validFreeAddresses)
		mh2 := registerNode(ctx, clusterID, "mh2")
		mh3 := registerNode(ctx, clusterID, "mh3")
		generateFullMeshConnectivity(ctx, "1.2.3.10", mh1, mh2, mh3, wh1, wh2, wh3)
		clusterReply, _ = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{
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
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh1.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *mh2.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *mh3.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusInsufficient, defaultWaitForClusterStateTimeout, clusterInsufficientStateInfo)

		By("Three master hosts -> state must be ready")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		waitForHostState(ctx, clusterID, *mh3.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)

		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout, clusterReadyStateInfo)

		By("Back to two master hosts -> state must be insufficient")
		cluster, updateErr := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(updateErr).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal(models.ClusterStatusInsufficient))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		By("Three master hosts -> state must be ready")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		waitForHostState(ctx, clusterID, *mh3.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)

		Expect(err).NotTo(HaveOccurred())
		waitForClusterState(ctx, clusterID, "ready", 60*time.Second, clusterReadyStateInfo)

		By("Back to two master hosts -> state must be insufficient")
		cluster, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *mh3.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal(models.ClusterStatusInsufficient))
		Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))

		_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(installer.NewGetClusterNotFound())))
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
			Timestamp:    1601853088,
			Routes:       common.TestDefaultRouteConfiguration,
		}
		h1 := &registerHost(clusterID).Host
		generateEssentialHostStepsWithInventory(ctx, h1, "h1", hwInfo)
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

		By("Register 3 more hosts with valid hw info")
		h2 := registerNode(ctx, clusterID, "h2")
		h3 := registerNode(ctx, clusterID, "h3")
		h4 := registerNode(ctx, clusterID, "h4")

		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4)
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusKnown, defaultWaitForClusterStateTimeout)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h2.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h3.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h4.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		By("validate that host 1 is insufficient")
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusInsufficient, defaultWaitForClusterStateTimeout)
	})

	It("unique_hostname_validation", func() {
		clusterID := *cluster.ID
		//define h1 as known master
		hosts := register3nodes(ctx, clusterID)
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *hosts[0].ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHost(clusterID, *hosts[0].ID)
		h2 := getHost(clusterID, *hosts[1].ID)
		h3 := getHost(clusterID, *hosts[2].ID)
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3)
		waitForHostState(ctx, clusterID, *h1.ID, "known", 60*time.Second)
		Expect(h1.RequestedHostname).Should(Equal("h1"))

		By("Registering host with same hostname")
		//after name clash --> h1 and h4 are insufficient
		h4 := registerNode(ctx, clusterID, "h1")
		h4 = getHost(clusterID, *h4.ID)
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4)
		waitForHostState(ctx, clusterID, *h1.ID, "insufficient", 60*time.Second)
		Expect(h4.RequestedHostname).Should(Equal("h1"))
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))

		By("Verifying install command")
		//install cluster should fail because only 2 hosts are known
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *hosts[1].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *hosts[2].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *h4.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())

		By("Registering one more host with same hostname")
		disabledHost := registerNode(ctx, clusterID, "h1")
		disabledHost = getHost(clusterID, *disabledHost.ID)
		waitForHostState(ctx, clusterID, *disabledHost.ID, models.HostStatusInsufficient,
			defaultWaitForHostStateTimeout)
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *disabledHost.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Changing hostname, verify host is known now")
		generateEssentialHostSteps(ctx, h4, "h4")
		waitForHostState(ctx, clusterID, *h4.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
		h4 = getHost(clusterID, *h4.ID)
		Expect(h4.RequestedHostname).Should(Equal("h4"))

		By("Disable host with the same hostname and verify h1 is known")
		_, err = userBMClient.Installer.DisableHost(ctx, &installer.DisableHostParams{
			ClusterID: clusterID,
			HostID:    *disabledHost.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		disabledHost = getHost(clusterID, *disabledHost.ID)
		Expect(*disabledHost.Status).Should(Equal(models.HostStatusDisabled))
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)

		By("add one more worker to get 2 functioning workers")
		h5 := registerNode(ctx, clusterID, "h5")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h5.ID, Role: models.HostRoleUpdateParamsWorker},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4, h5)

		By("waiting for cluster to be in ready state")
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

		By("Verify install after disabling the host with same hostname")
		_, err = userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})

	It("localhost is not valid", func() {
		localhost := "localhost"
		clusterID := *cluster.ID

		hosts := register3nodes(ctx, clusterID)
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *hosts[0].ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHost(clusterID, *hosts[0].ID)
		waitForHostState(ctx, clusterID, *h1.ID, "known", 60*time.Second)
		Expect(h1.RequestedHostname).Should(Equal("h1"))

		By("Changing hostname reply to localhost")
		generateEssentialHostSteps(ctx, h1, localhost)
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusInsufficient, 60*time.Second)
		h1Host := getHost(clusterID, *h1.ID)
		Expect(h1Host.RequestedHostname).Should(Equal(localhost))

		By("Setting hostname to valid name")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
				{ID: *h1Host.ID, Hostname: "reqh0"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusKnown, 60*time.Second)

		By("Setting hostname to localhost")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
				{ID: *h1Host.ID, Hostname: localhost},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusInsufficient, 60*time.Second)

	})

	It("different_roles_stages", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRoles(clusterID, 5)
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
		hosts := register3nodes(ctx, clusterID)
		_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *hosts[0].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *hosts[1].ID, Role: models.HostRoleUpdateParamsMaster},
				{ID: *hosts[2].ID, Role: models.HostRoleUpdateParamsMaster},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		h1 := getHost(clusterID, *hosts[0].ID)
		h2 := getHost(clusterID, *hosts[1].ID)
		h3 := getHost(clusterID, *hosts[2].ID)
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusKnown, time.Minute)
		waitForHostState(ctx, clusterID, *h2.ID, models.HostStatusKnown, time.Minute)
		waitForHostState(ctx, clusterID, *h3.ID, models.HostStatusKnown, time.Minute)
		// update requested hostnames
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
				{ID: *hosts[0].ID, Hostname: "reqh0"},
				{ID: *hosts[1].ID, Hostname: "reqh1"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		// check hostnames were updated
		h1 = getHost(clusterID, *h1.ID)
		h2 = getHost(clusterID, *h2.ID)
		h3 = getHost(clusterID, *h3.ID)
		Expect(h1.RequestedHostname).Should(Equal("reqh0"))
		Expect(h2.RequestedHostname).Should(Equal("reqh1"))
		Expect(*h1.Status).Should(Equal(models.HostStatusKnown))
		Expect(*h2.Status).Should(Equal(models.HostStatusKnown))
		Expect(*h3.Status).Should(Equal(models.HostStatusKnown))

		// register new host with the same name in inventory
		By("Registering new host with same hostname as in node's inventory")
		h4 := registerNode(ctx, clusterID, "h3")
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4)
		h4 = getHost(clusterID, *h4.ID)
		waitForHostState(ctx, clusterID, *h4.ID, models.HostStatusInsufficient, time.Minute)
		waitForHostState(ctx, clusterID, *h3.ID, models.HostStatusInsufficient, time.Minute)

		By("Check cluster install fails on validation")
		_, err = userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())

		By("Registering new host with same hostname as in node's requested_hostname")
		h5 := registerNode(ctx, clusterID, "reqh0")
		h5 = getHost(clusterID, *h5.ID)
		generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4, h5)
		waitForHostState(ctx, clusterID, *h5.ID, models.HostStatusInsufficient, time.Minute)
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusInsufficient, time.Minute)

		By("Change requested hostname of an insufficient node")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
					{ID: *hosts[0].ID, Hostname: "reqh0new"},
				},
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{ID: *h5.ID, Role: models.HostRoleUpdateParamsWorker},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForHostState(ctx, clusterID, *h1.ID, models.HostStatusKnown, time.Minute)
		waitForHostState(ctx, clusterID, *h5.ID, models.HostStatusKnown, time.Minute)

		By("change the requested hostname of the insufficient node")
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
					{ID: *h3.ID, Hostname: "reqh2"},
				},
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{ID: *h4.ID, Role: models.HostRoleUpdateParamsWorker},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		waitForHostState(ctx, clusterID, *h3.ID, models.HostStatusKnown, time.Minute)
		waitForClusterState(ctx, clusterID, models.ClusterStatusReady, time.Minute, clusterReadyStateInfo)
		_, err = userBMClient.Installer.InstallCluster(ctx, &installer.InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("Preflight Cluster Requirements", func() {
	var (
		ctx                   context.Context
		clusterID             strfmt.UUID
		masterOCPRequirements = models.ClusterHostRequirementsDetails{
			CPUCores:                         4,
			DiskSizeGb:                       120,
			RAMMib:                           16384,
			InstallationDiskSpeedThresholdMs: 10,
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(100),
			PacketLossPercentage:             pointer.Float64Ptr(0),
		}
		workerOCPRequirements = models.ClusterHostRequirementsDetails{
			CPUCores:                         2,
			DiskSizeGb:                       120,
			RAMMib:                           8192,
			InstallationDiskSpeedThresholdMs: 10,
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(1000),
			PacketLossPercentage:             pointer.Float64Ptr(10),
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
	)

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())

		clusterID = cID
	})

	AfterEach(func() {
		clearDB()
	})

	It("should be reported for cluster", func() {
		params := installer.GetPreflightRequirementsParams{ClusterID: clusterID}

		response, err := userBMClient.Installer.GetPreflightRequirements(ctx, &params)

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
		Expect(requirements.Operators).To(HaveLen(3))
		for _, op := range requirements.Operators {
			switch op.OperatorName {
			case lso.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(models.ClusterHostRequirementsDetails{}))
			case ocs.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterOCSRequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerOCSRequirements))
			case cnv.Operator.Name:
				Expect(*op.Requirements.Master.Quantitative).To(BeEquivalentTo(masterCNVRequirements))
				Expect(*op.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerCNVRequirements))
			default:
				Fail("Unexpected operator")
			}
		}
	})
})

func checkUpdateAtWhileStatic(ctx context.Context, clusterID strfmt.UUID) {
	clusterReply, getErr := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{
		ClusterID: clusterID,
	})
	Expect(getErr).ToNot(HaveOccurred())
	preSecondRefreshUpdatedTime := clusterReply.Payload.UpdatedAt
	time.Sleep(30 * time.Second)
	clusterReply, getErr = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{
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

func FailCluster(ctx context.Context, clusterID strfmt.UUID, reason int) strfmt.UUID {
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

	updateProgressWithInfo(hostID, clusterID, installStep, installInfo)
	host := getHost(clusterID, hostID)
	Expect(*host.Status).Should(Equal("error"))
	Expect(*host.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", installStep, installInfo)))
	return hostID
}

var _ = Describe("cluster install, with default network params", func() {
	var (
		ctx     = context.Background()
		cluster *models.Cluster
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		By("Register cluster")
		registerClusterReply, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
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
	})

	It("install cluster", func() {
		clusterID := *cluster.ID
		registerHostsAndSetRoles(clusterID, 5)
		rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		c := rep.GetPayload()
		startTimeInstalling := c.InstallStartedAt
		startTimeInstalled := c.InstallCompletedAt

		c = installCluster(clusterID)
		Expect(len(c.Hosts)).Should(Equal(5))
		Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		for _, host := range c.Hosts {
			waitForHostState(ctx, clusterID, *host.ID, "installing", 10*time.Second)
		}
		// fake installation completed
		for _, host := range c.Hosts {
			updateProgress(*host.ID, clusterID, models.HostStageDone)
		}

		waitForClusterState(ctx, clusterID, "finalizing", defaultWaitForClusterStateTimeout, "Finalizing cluster installation")
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)

		rep, err = userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})

		Expect(err).NotTo(HaveOccurred())
		c = rep.GetPayload()
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInstalled))
		Expect(c.InstallCompletedAt).ShouldNot(Equal(startTimeInstalled))
		Expect(c.InstallCompletedAt).Should(Equal(c.StatusUpdatedAt))
	})
	Context("fail disk speed", func() {
		It("first host", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, 5)
			rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, c.Hosts[1:], c.Hosts[:1])
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
		It("all hosts", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, 5)
			rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, nil, c.Hosts)
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
		It("last host", func() {
			clusterID := *cluster.ID
			registerHostsAndSetRoles(clusterID, 5)
			rep, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
			Expect(err).NotTo(HaveOccurred())
			c := rep.GetPayload()
			startTimeInstalling := c.InstallStartedAt

			c = tryInstallClusterWithDiskResponses(clusterID, nil, c.Hosts[len(c.Hosts)-1:])
			Expect(len(c.Hosts)).Should(Equal(5))
			Expect(c.InstallStartedAt).ShouldNot(Equal(startTimeInstalling))
		})
	})
})

var _ = Describe("Verify ISO is deleted on cluster de-registration", func() {
	var (
		ctx       context.Context = context.Background()
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		var err error
		clusterID, err = registerCluster(ctx, userBMClient, "test-deregister-cluster", pullSecret)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		clearDB()
	})

	Context("Deregister cluster deletes cluster resources test", func() {
		It("Deregister cluster deletes discovery image from Filesystem test", func() {
			By("Generate discovery image for cluster")
			imageType := models.ImageTypeMinimalIso
			_, err := userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
				ClusterID: clusterID,
				ImageCreateParams: &models.ImageCreateParams{
					ImageType: imageType,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			verifyEventExistence(clusterID, fmt.Sprintf("Image type is \"%s\"", imageType))

			By("verify discovery-image existence")
			file, err := ioutil.TempFile("", "tmp")
			if err != nil {
				log.Fatal(err)
			}
			defer os.Remove(file.Name())
			_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{ClusterID: clusterID}, file)
			Expect(err).NotTo(HaveOccurred())

			By("deregister cluster")
			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
				ClusterID: clusterID,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify discovery-image cannot be downloaded")
			_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{ClusterID: clusterID}, file)
			Expect(err).To(HaveOccurred())
		})
	})
})

func registerHostsAndSetRoles(clusterID strfmt.UUID, numHosts int) []*models.Host {
	ctx := context.Background()
	hosts := make([]*models.Host, 0)

	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := registerNode(ctx, clusterID, hostname)
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
	generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
	apiVip := ""
	ingressVip := ""
	_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVip:            &apiVip,
			IngressVip:        &ingressVip,
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	apiVip = "1.2.3.8"
	ingressVip = "1.2.3.9"
	_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			APIVip:     &apiVip,
			IngressVip: &ingressVip,
		},
		ClusterID: clusterID,
	})

	Expect(err).NotTo(HaveOccurred())

	for _, host := range hosts {
		waitForHostState(ctx, clusterID, *host.ID, models.HostStatusKnown, defaultWaitForHostStateTimeout)
	}

	waitForClusterState(ctx, clusterID, models.ClusterStatusReady, 60*time.Second, clusterReadyStateInfo)

	return hosts
}

func registerHostsAndSetRolesDHCP(clusterID strfmt.UUID, numHosts int) []*models.Host {
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
		_, err = agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				StepType: models.StepTypeDhcpLeaseAllocate,
				Output:   string(b),
				StepID:   string(models.StepTypeDhcpLeaseAllocate),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}
	for i := 0; i < numHosts; i++ {
		hostname := fmt.Sprintf("h%d", i)
		host := registerNode(ctx, clusterID, hostname)
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
	generateFullMeshConnectivity(ctx, "1.2.3.10", hosts...)
	_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			MachineNetworkCidr: swag.String("1.2.3.0/24"),
		},
		ClusterID: clusterID,
	})
	Expect(err).ToNot(HaveOccurred())
	for _, h := range hosts {
		generateDhcpStepReply(h, apiVip, ingressVip)
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
	_, err = agentBMClient.Installer.PostStepReply(ctx, &installer.PostStepReplyParams{
		ClusterID: h.ClusterID,
		HostID:    *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(fa),
			StepID:   string(models.StepTypeConnectivityCheck),
			StepType: models.StepTypeConnectivityCheck,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func generateFullMeshConnectivity(ctx context.Context, startIPAddress string, hosts ...*models.Host) {

	ip := net.ParseIP(startIPAddress)
	hostToAddr := make(map[strfmt.UUID]string)

	for _, h := range hosts {
		hostToAddr[*h.ID] = ip.String()
		ip[len(ip)-1]++
	}

	var connectivityReport models.ConnectivityReport
	for _, h := range hosts {

		l2Connectivity := make([]*models.L2Connectivity, 0)
		l3Connectivity := make([]*models.L3Connectivity, 0)
		for id, addr := range hostToAddr {

			if id == *h.ID {
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
