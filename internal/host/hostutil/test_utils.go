package hostutil

import (
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/models"

	. "github.com/onsi/gomega"
)

func GetHostFromDB(hostId, clusterId strfmt.UUID, db *gorm.DB) *models.Host {
	var host models.Host
	Expect(db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error).ShouldNot(HaveOccurred())
	return &host
}

func GenerateTestCluster(clusterID strfmt.UUID, machineNetworkCidr string) common.Cluster {
	return common.Cluster{
		Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: machineNetworkCidr,
		},
	}
}

/* Host */

func GenerateTestHost(hostID, clusterID strfmt.UUID, state string) models.Host {
	return GenerateTestHostByKind(hostID, clusterID, state, models.HostKindHost)
}

func GenerateTestHostAddedToCluster(hostID, clusterID strfmt.UUID, state string) models.Host {
	return GenerateTestHostByKind(hostID, clusterID, state, models.HostKindAddToExistingClusterHost)
}

func GenerateTestHostByKind(hostID, clusterID strfmt.UUID, state, kind string) models.Host {
	return models.Host{
		ID:                 &hostID,
		ClusterID:          clusterID,
		Status:             swag.String(state),
		Inventory:          common.GenerateTestDefaultInventory(),
		Role:               models.HostRoleWorker,
		Kind:               swag.String(kind),
		CheckedInAt:        strfmt.DateTime(time.Now()),
		APIVipConnectivity: generateTestAPIVIpConnectivity(),
	}
}

func generateTestAPIVIpConnectivity() string {
	checkAPIResponse := models.APIVipConnectivityResponse{
		IsSuccess: true,
	}
	bytes, err := json.Marshal(checkAPIResponse)
	if err != nil {
		return ""
	}
	return string(bytes)
}

/* Inventory */

func GenerateMasterInventory() string {
	return GenerateMasterInventoryWithHostname("master-hostname")
}

func GenerateMasterInventoryV6() string {
	return GenerateMasterInventoryWithHostnameV6("master-hostname")
}

func GenerateMasterInventoryWithHostname(hostname string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8},
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
		Memory:       &models.Memory{PhysicalBytes: hardware.GibToBytes(16)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Timestamp:    1601835002,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateMasterInventoryWithHostnameV6(hostname string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8},
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
		Memory:       &models.Memory{PhysicalBytes: hardware.GibToBytes(16)},
		Hostname:     hostname,
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Timestamp:    1601835002,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}
