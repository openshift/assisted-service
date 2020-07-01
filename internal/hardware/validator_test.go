package hardware

import (
	"encoding/json"
	"testing"

	"github.com/filanov/bm-inventory/internal/validators"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hardware Validator tests Suite")
}

var _ = Describe("hardware_validator", func() {
	var (
		hwvalidator      Validator
		host1            *models.Host
		host2            *models.Host
		host3            *models.Host
		inventory        *models.Inventory
		cluster          *common.Cluster
		validDiskSize    = int64(128849018880)
		notValidDiskSize = int64(108849018880)
	)
	BeforeEach(func() {
		var cfg ValidatorCfg
		status := models.HostStatusKnown
		Expect(envconfig.Process("myapp", &cfg)).ShouldNot(HaveOccurred())
		hwvalidator = NewValidator(logrus.New(), cfg)
		id1 := strfmt.UUID(uuid.New().String())
		id2 := strfmt.UUID(uuid.New().String())
		id3 := strfmt.UUID(uuid.New().String())
		clusterID := strfmt.UUID(uuid.New().String())
		host1 = &models.Host{ID: &id1, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname1"}
		host2 = &models.Host{ID: &id2, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname2"}
		host3 = &models.Host{ID: &id3, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname3"}
		inventory = &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
			Disks: []*models.Disk{
				{DriveType: "ODD", Name: "loop0", SizeBytes: validDiskSize},
				{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
		}
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: "1.2.3.0/24",
		}}
		cluster.Hosts = append(cluster.Hosts, host1)
		cluster.Hosts = append(cluster.Hosts, host2)
		cluster.Hosts = append(cluster.Hosts, host3)
	})

	It("sufficient_hw", func() {
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host1.Role = role
			sufficient(hwvalidator.IsSufficient(host1, cluster))
		}
	})

	It("insufficient_minimal_hw_requirements", func() {
		inventory.CPU = &models.CPU{Count: 1}
		inventory.Memory = &models.Memory{PhysicalBytes: int64(3 * units.GiB)}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host1.Role = role
			insufficient(hwvalidator.IsSufficient(host1, cluster))
		}
	})

	It("insufficient_master_but_valid_worker", func() {
		inventory.CPU = &models.CPU{Count: 8}
		inventory.Memory = &models.Memory{PhysicalBytes: int64(8 * units.GiB)}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		host1.Role = "master"
		insufficient(hwvalidator.IsSufficient(host1, cluster))
		host1.Role = "worker"
		sufficient(hwvalidator.IsSufficient(host1, cluster))
	})

	It("insufficient_number_of_valid_disks", func() {
		inventory.Disks = []*models.Disk{
			// Not enough size
			{DriveType: "HDD", Name: "sdb", SizeBytes: notValidDiskSize},
			// Removable
			{DriveType: "FDD", Name: "sda", SizeBytes: validDiskSize},
			// Filtered Name
			{DriveType: "HDD", Name: "nvme01fs", SizeBytes: validDiskSize},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())

		host1.Inventory = string(hw)
		insufficient(hwvalidator.IsSufficient(host1, cluster))

		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).To(HaveOccurred())
		Expect(disks).To(BeNil())
	})

	It("validate_disk_list_return_order", func() {
		nvmename := "nvme01fs"
		inventory.Disks = []*models.Disk{
			// Not disk type
			{DriveType: "ODD", Name: "aaa", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize + 1},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize + 100},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
			{DriveType: "SDD", Name: nvmename, SizeBytes: validDiskSize},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("sdh"))
		Expect(len(disks)).Should(Equal(3))
		Expect(isBlockDeviceNameInlist(disks, nvmename)).Should(Equal(false))
	})

	It("invalid_hw_info", func() {
		host1.Inventory = "not a valid json"
		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host1.Role = role
			reply, err := hwvalidator.IsSufficient(host1, cluster)
			Expect(err).To(HaveOccurred())
			Expect(reply).To(BeNil())
		}
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).To(HaveOccurred())
		Expect(disks).To(BeNil())
	})

	It("requested_hostnames_unique_invneotry_hostnames_not_unique", func() {
		inventory.Hostname = "same_hostname"
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		host2.Inventory = string(hw)
		host3.Inventory = string(hw)
		sufficient(hwvalidator.IsSufficient(host1, cluster))
	})

	It("requested_hostnames_not_unique", func() {
		host1.RequestedHostname = "non_unique_hostname"
		host3.RequestedHostname = "non_unique_hostname"
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		host2.Inventory = string(hw)
		host3.Inventory = string(hw)
		insufficient(hwvalidator.IsSufficient(host1, cluster))
	})

	It("requested_hostnames_empty_inventory_non_unique", func() {
		host1.RequestedHostname = ""
		host2.RequestedHostname = ""
		host3.RequestedHostname = ""
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		host2.Inventory = string(hw)
		host3.Inventory = string(hw)
		insufficient(hwvalidator.IsSufficient(host1, cluster))
	})

})

func sufficient(reply *validators.IsSufficientReply, err error) {
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, reply.IsSufficient).To(BeTrue())
	ExpectWithOffset(1, reply.Reason).Should(Equal(""))
}

func insufficient(reply *validators.IsSufficientReply, err error) {
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, reply.IsSufficient).To(BeFalse())
	ExpectWithOffset(1, reply.Reason).ShouldNot(Equal(""))
}

func isBlockDeviceNameInlist(disks []*models.Disk, name string) bool {
	for _, disk := range disks {
		// Valid disk: type=disk, not removable, not readonly and size bigger than minimum required
		if disk.Name == name {
			return true
		}
	}
	return false
}
