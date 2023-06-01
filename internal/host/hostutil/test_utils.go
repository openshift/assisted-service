package hostutil

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"gorm.io/gorm"
)

func GetHostFromDB(hostId, infraEnvId strfmt.UUID, db *gorm.DB) *common.Host {
	var host common.Host
	Expect(db.First(&host, "id = ? and infra_env_id = ?", hostId, infraEnvId).Error).ShouldNot(HaveOccurred())
	return &host
}

func GenerateTestClusterWithMachineNetworks(clusterID strfmt.UUID, machineNetworks []*models.MachineNetwork) common.Cluster {
	return common.Cluster{
		Cluster: models.Cluster{
			ID:              &clusterID,
			MachineNetworks: machineNetworks,
			Platform:        &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			Kind:            swag.String(models.ClusterKindCluster),
			DiskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			OpenshiftVersion: lvm.LvmsMinOpenshiftVersion,
		},
	}
}

func GenerateTestCluster(clusterID strfmt.UUID) common.Cluster {
	return common.Cluster{
		Cluster: models.Cluster{
			Name:            "test-cluster",
			BaseDNSDomain:   "example.com",
			ID:              &clusterID,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			Platform:        &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			Kind:            swag.String(models.ClusterKindCluster),
			DiskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			OpenshiftVersion: lvm.LvmsMinOpenshiftVersion,
		},
	}
}

func GenerateTestClusterWithPlatform(clusterID strfmt.UUID, machineNetworks []*models.MachineNetwork, platform *models.Platform) common.Cluster {
	cluster := GenerateTestClusterWithMachineNetworks(clusterID, machineNetworks)
	cluster.Platform = platform
	return cluster
}

func GenerateTestInfraEnv(infraEnvID strfmt.UUID) *common.InfraEnv {
	return &common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID: &infraEnvID,
		},
	}
}

/* Host */

func GenerateUnassignedTestHost(hostID, infraEnvID strfmt.UUID, state string) models.Host {
	return GenerateTestHostByKind(hostID, infraEnvID, nil, state, models.HostKindHost, models.HostRoleWorker)
}

func GenerateTestHost(hostID, infraEnvID, clusterID strfmt.UUID, state string) models.Host {
	return GenerateTestHostByKind(hostID, infraEnvID, &clusterID, state, models.HostKindHost, models.HostRoleWorker)
}

func GenerateTestHostAddedToCluster(hostID, infraEnvID, clusterID strfmt.UUID, state string) models.Host {
	return GenerateTestHostByKind(hostID, infraEnvID, &clusterID, state, models.HostKindAddToExistingClusterHost, models.HostRoleWorker)
}

func GenerateTestHostByKind(hostID, infraEnvID strfmt.UUID, clusterID *strfmt.UUID, state, kind string, role models.HostRole) models.Host {
	aMinuteAgoTime := time.Now().Add(-time.Minute)
	aMinuteAgo := strfmt.DateTime(aMinuteAgoTime)
	return models.Host{
		ID:              &hostID,
		InfraEnvID:      infraEnvID,
		ClusterID:       clusterID,
		Status:          swag.String(state),
		Inventory:       common.GenerateTestInventory(),
		Role:            role,
		SuggestedRole:   role,
		Kind:            swag.String(kind),
		CheckedInAt:     aMinuteAgo,
		RegisteredAt:    aMinuteAgo,
		StatusUpdatedAt: aMinuteAgo,
		Progress: &models.HostProgressInfo{
			StageStartedAt: aMinuteAgo,
			StageUpdatedAt: aMinuteAgo,
		},
		APIVipConnectivity:    GenerateTestAPIConnectivityResponseSuccessString(""),
		Connectivity:          GenerateTestConnectivityReport(),
		DiscoveryAgentVersion: defaultAgentImage,
		Timestamp:             aMinuteAgoTime.Unix(),
	}
}

func GenerateTestHostWithInfraEnv(hostID, infraEnvID strfmt.UUID, state string, role models.HostRole) models.Host {
	aMinuteAgoTime := time.Now().Add(-time.Minute)
	aMinuteAgo := strfmt.DateTime(aMinuteAgoTime)
	return models.Host{
		ID:                    &hostID,
		InfraEnvID:            infraEnvID,
		Status:                swag.String(state),
		Inventory:             common.GenerateTestDefaultInventory(),
		Role:                  role,
		Kind:                  swag.String(models.HostKindHost),
		CheckedInAt:           aMinuteAgo,
		RegisteredAt:          aMinuteAgo,
		StatusUpdatedAt:       aMinuteAgo,
		APIVipConnectivity:    GenerateTestAPIConnectivityResponseSuccessString(""),
		Connectivity:          GenerateTestConnectivityReport(),
		DiscoveryAgentVersion: defaultAgentImage,
		Timestamp:             aMinuteAgoTime.Unix(),
	}
}

func GenerateTestHostWithNetworkAddress(hostID, infraEnvID, clusterID strfmt.UUID, role models.HostRole, status string, netAddr common.NetAddress) *models.Host {
	aMinuteAgoTime := time.Now().Add(-time.Minute)
	aMinuteAgo := strfmt.DateTime(aMinuteAgoTime)
	h := models.Host{
		ID:                &hostID,
		RequestedHostname: netAddr.Hostname,
		ClusterID:         &clusterID,
		InfraEnvID:        infraEnvID,
		Status:            swag.String(status),
		Inventory:         common.GenerateTestInventoryWithNetwork(netAddr),
		Role:              role,
		Kind:              swag.String(models.HostKindHost),
		CheckedInAt:       aMinuteAgo,
		RegisteredAt:      aMinuteAgo,
		StatusUpdatedAt:   aMinuteAgo,
		Progress: &models.HostProgressInfo{
			StageStartedAt: aMinuteAgo,
			StageUpdatedAt: aMinuteAgo,
		},
		APIVipConnectivity:    GenerateTestAPIConnectivityResponseSuccessString(""),
		DiscoveryAgentVersion: defaultAgentImage,
		Timestamp:             aMinuteAgoTime.Unix(),
	}
	return &h
}

func GenerateTestConnectivityReport() string {
	c := models.ConnectivityReport{RemoteHosts: []*models.ConnectivityRemoteHost{}}
	b, err := json.Marshal(&c)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

func GenerateL3ConnectivityReport(hosts []*models.Host, latency float64, packetLoss float64) *models.ConnectivityReport {
	con := models.ConnectivityReport{}
	for _, h := range hosts {
		var inv models.Inventory
		Expect(json.Unmarshal([]byte(h.Inventory), &inv)).NotTo(HaveOccurred())
		var ipAddr string
		if len(inv.Interfaces[0].IPV4Addresses) != 0 {
			ip, _, err := net.ParseCIDR(inv.Interfaces[0].IPV4Addresses[0])
			Expect(err).NotTo(HaveOccurred())
			ipAddr = ip.String()
		} else if len(inv.Interfaces[0].IPV6Addresses) != 0 {
			ip, _, err := net.ParseCIDR(inv.Interfaces[0].IPV6Addresses[0])
			Expect(err).NotTo(HaveOccurred())
			ipAddr = ip.String()
		}
		Expect(ipAddr).NotTo(BeEmpty())
		l3 := models.L3Connectivity{Successful: true, AverageRTTMs: latency, PacketLossPercentage: packetLoss, RemoteIPAddress: ipAddr}
		con.RemoteHosts = append(con.RemoteHosts, &models.ConnectivityRemoteHost{HostID: *h.ID, L3Connectivity: []*models.L3Connectivity{&l3}})
	}
	return &con
}

func generateTestAPIVIpConnectivityResponseString(ignition string, isSuccess bool) string {
	checkAPIResponse := models.APIVipConnectivityResponse{
		IsSuccess: isSuccess,
		Ignition:  ignition,
	}
	bytes, err := json.Marshal(checkAPIResponse)
	Expect(err).To(Not(HaveOccurred()))
	return string(bytes)
}

func GenerateTestAPIConnectivityResponseSuccessString(ignition string) string {
	return generateTestAPIVIpConnectivityResponseString(ignition, true)
}

func GenerateTestAPIConnectivityResponseFailureString(ignition string) string {
	return generateTestAPIVIpConnectivityResponseString(ignition, false)
}

func getTangResponse(url string) models.TangServerResponse {
	return models.TangServerResponse{
		TangURL: url,
		Payload: "some_fake_payload",
		Signatures: []*models.TangServerSignatures{
			{
				Signature: "some_fake_signature1",
				Protected: "foobar1",
			},
			{
				Signature: "some_fake_signature2",
				Protected: "foobar2",
			},
		},
	}
}

func GenerateTestTangConnectivity(success bool) string {
	checkAPIResponse := models.TangConnectivityResponse{
		IsSuccess:          false,
		TangServerResponse: nil,
	}

	if success {
		tangResponse := getTangResponse("http://tang.example.com:7500")
		checkAPIResponse = models.TangConnectivityResponse{
			IsSuccess:          true,
			TangServerResponse: []*models.TangServerResponse{&tangResponse},
		}
	}

	bytes, err := json.Marshal(checkAPIResponse)
	Expect(err).To(Not(HaveOccurred()))
	return string(bytes)
}

/* Inventory */

func GenerateMasterInventoryWithSystemPlatform(systemPlatform string) string {
	return GenerateMasterInventoryWithHostnameAndCpuFlags("master-hostname", []string{"vmx"}, systemPlatform)
}

func GenerateMasterInventory() string {
	return GenerateMasterInventoryWithHostname("master-hostname")
}

func GenerateMasterInventoryV6() string {
	return GenerateMasterInventoryWithHostnameV6("master-hostname")
}

func GenerateMasterInventoryDualStack() string {
	return GenerateMasterInventoryWithHostnameDualStack("master-hostname")
}

func GenerateMasterInventoryWithHostname(hostname string) string {
	return GenerateMasterInventoryWithHostnameAndCpuFlags(hostname, []string{"vmx"}, "RHEL")
}

func GenerateMasterInventoryWithHostnameV6(hostname string) string {
	return GenerateMasterInventoryWithHostnameAndCpuFlagsV6(hostname, []string{"vmx"})
}

func GenerateMasterInventoryWithHostnameDualStack(hostname string) string {
	return GenerateMasterInventoryWithHostnameAndCpuFlagsDualStack(hostname, []string{"vmx"})
}

func GenerateMasterInventoryWithHostnameAndCpuFlags(hostname string, cpuflags []string, systemPlatform string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: cpuflags, Architecture: models.ClusterCPUArchitectureX8664},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
				ID:        "/dev/disk/by-id/test-disk-id",
				Name:      "test-disk",
				Serial:    "test-serial",
				Path:      "/dev/test-disk",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: systemPlatform, SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithNetworks(ips ...string) string {
	var interfaces []*models.Interface
	for i := range ips {
		interfaces = append(interfaces, &models.Interface{
			Name: fmt.Sprintf("eth%d", i),
			IPV4Addresses: []string{
				ips[i],
			},
		})
	}
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
				ID:        "/dev/disk/by-id/test-disk-id",
				Name:      "test-disk",
				Serial:    "test-serial",
				Path:      "/dev/test-disk",
			},
		},
		Interfaces:   interfaces,
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     "master1",
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithNetworksOnSameInterface(v4ips []string, v6ips []string) string {
	interfaces := []*models.Interface{
		{
			Name:          "eth0",
			IPV4Addresses: v4ips,
			IPV6Addresses: v6ips,
		},
	}
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
				ID:        "/dev/disk/by-id/test-disk-id",
				Name:      "test-disk",
				Serial:    "test-serial",
				Path:      "/dev/test-disk",
			},
		},
		Interfaces:   interfaces,
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     "master1",
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithHostnameAndCpuFlagsV6(hostname string, cpuflags []string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: cpuflags},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithHostnameAndCpuFlagsDualStack(hostname string, cpuflags []string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: cpuflags},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateInventoryWithResources(cpu, memory int64, hostname string, gpus ...*models.Gpu) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: cpu, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
			},
		},
		Gpus: gpus,
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(memory), UsableBytes: conversions.GibToBytes(memory)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateInventoryWithResourcesAndMultipleDisk(cpu, memory int64, hostname string, gpus ...*models.Gpu) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: cpu, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
			},
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeSSD,
			},
		},
		Gpus: gpus,
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(memory), UsableBytes: conversions.GibToBytes(memory)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateInventoryWithResourcesWithBytes(cpu, physicalMemory int64, usableMemory int64, hostname string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: cpu, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: models.DriveTypeHDD,
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: physicalMemory, UsableBytes: usableMemory},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateIPv4Addresses(count int, machineCIDR string) []string {
	ipAddress, mask, err := net.ParseCIDR(machineCIDR)
	Expect(err).NotTo(HaveOccurred())
	sz, _ := mask.Mask.Size()
	return generateIPAddresses(count, ipAddress.To4(), sz)
}

func GenerateIPv6Addresses(count int, machineCIDR string) []string {
	ipAddress, mask, err := net.ParseCIDR(machineCIDR)
	Expect(err).NotTo(HaveOccurred())
	sz, _ := mask.Mask.Size()
	return generateIPAddresses(count, ipAddress.To16(), sz)
}

func generateIPAddresses(count int, ipAddress net.IP, mask int) []string {
	ret := make([]string, count)
	for i := 0; i < count; i++ {
		common.IncrementIP(ipAddress)
		ret[i] = fmt.Sprintf("%s/%d", ipAddress, mask)
	}
	return ret
}

const defaultAgentImage = "quay.io/edge-infrastructure/assisted-installer-agent:latest"
