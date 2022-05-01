package hostutil

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"gorm.io/gorm"
)

func GetHostFromDB(hostId, infraEnvId strfmt.UUID, db *gorm.DB) *common.Host {
	var host common.Host
	Expect(db.First(&host, "id = ? and infra_env_id = ?", hostId, infraEnvId).Error).ShouldNot(HaveOccurred())
	return &host
}

func GenerateTestCluster(clusterID strfmt.UUID, machineNetworks []*models.MachineNetwork) common.Cluster {
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
		},
	}
}

func GenerateTestClusterWithPlatform(clusterID strfmt.UUID, machineNetworks []*models.MachineNetwork, platform *models.Platform) common.Cluster {
	cluster := GenerateTestCluster(clusterID, machineNetworks)
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
	now := strfmt.DateTime(time.Now())
	return models.Host{
		ID:              &hostID,
		InfraEnvID:      infraEnvID,
		ClusterID:       clusterID,
		Status:          swag.String(state),
		Inventory:       common.GenerateTestDefaultInventory(),
		Role:            role,
		SuggestedRole:   role,
		Kind:            swag.String(kind),
		CheckedInAt:     now,
		StatusUpdatedAt: now,
		Progress: &models.HostProgressInfo{
			StageStartedAt: now,
			StageUpdatedAt: now,
		},
		APIVipConnectivity: GenerateTestAPIVIpConnectivity(""),
		Connectivity:       GenerateTestConnectivityReport(),
	}
}

func GenerateTestHostWithInfraEnv(hostID, infraEnvID strfmt.UUID, state string, role models.HostRole) models.Host {
	now := strfmt.DateTime(time.Now())
	return models.Host{
		ID:                 &hostID,
		InfraEnvID:         infraEnvID,
		Status:             swag.String(state),
		Inventory:          common.GenerateTestDefaultInventory(),
		Role:               role,
		Kind:               swag.String(models.HostKindHost),
		CheckedInAt:        now,
		StatusUpdatedAt:    now,
		APIVipConnectivity: GenerateTestAPIVIpConnectivity(""),
		Connectivity:       GenerateTestConnectivityReport(),
	}
}

func GenerateTestHostWithNetworkAddress(hostID, infraEnvID, clusterID strfmt.UUID, role models.HostRole, status string, netAddr common.NetAddress) *models.Host {
	now := strfmt.DateTime(time.Now())
	h := models.Host{
		ID:                &hostID,
		RequestedHostname: netAddr.Hostname,
		ClusterID:         &clusterID,
		InfraEnvID:        infraEnvID,
		Status:            swag.String(status),
		Inventory:         common.GenerateTestInventoryWithNetwork(netAddr),
		Role:              role,
		Kind:              swag.String(models.HostKindHost),
		CheckedInAt:       now,
		StatusUpdatedAt:   now,
		Progress: &models.HostProgressInfo{
			StageStartedAt: now,
			StageUpdatedAt: now,
		},
		APIVipConnectivity: GenerateTestAPIVIpConnectivity(""),
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
		con.RemoteHosts = append(con.RemoteHosts, &models.ConnectivityRemoteHost{HostID: strfmt.UUID(uuid.New().String()), L3Connectivity: []*models.L3Connectivity{&l3}})
	}
	return &con
}

func GenerateTestAPIVIpConnectivity(ignition string) string {
	checkAPIResponse := models.APIVipConnectivityResponse{
		IsSuccess: true,
		Ignition:  ignition,
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
		CPU: &models.CPU{Count: 8, Flags: cpuflags},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
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
		Timestamp:    1601835002,
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithNetworksOnSameInterface(ips ...string) string {
	interfaces := []*models.Interface{
		{
			Name:          "eth0",
			IPV4Addresses: ips,
		},
	}
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8, Flags: []string{"vmx"}},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
			},
			{
				SizeBytes: 128849018880,
				DriveType: "SDD",
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
		Timestamp:    1601835002,
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
				DriveType: "HDD",
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
		Timestamp:    1601835002,
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
