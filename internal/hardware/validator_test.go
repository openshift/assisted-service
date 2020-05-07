package hardware

import (
	"encoding/json"
	"testing"

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
	RunSpecs(t, "Subsystem Suite")
}

var _ = Describe("hardware_validator", func() {
	var (
		hwvalidator      Validator
		host             *models.Host
		hwInfo           *models.Introspection
		validDiskSize    = int64(128849018880)
		notValidDiskSize = int64(108849018880)
	)
	BeforeEach(func() {
		var cfg ValidatorCfg
		Expect(envconfig.Process("myapp", &cfg)).ShouldNot(HaveOccurred())
		hwvalidator = NewValidator(cfg)
		id := strfmt.UUID(uuid.New().String())
		host = &models.Host{Base: models.Base{ID: &id}, ClusterID: strfmt.UUID(uuid.New().String())}
		hwInfo = &models.Introspection{
			CPU:    &models.CPU{Cpus: 16},
			Memory: []*models.Memory{{Name: "Mem", Total: int64(32 * units.GiB)}},
			BlockDevices: []*models.BlockDevice{
				{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "loop0", ReadOnly: true, RemovableDevice: 1, Size: validDiskSize},
				{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize}},
		}
	})

	It("sufficient_hw", func() {
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		host.HardwareInfo = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			sufficient(hwvalidator.IsSufficient(host))
		}
	})

	It("insufficient_minimal_hw_requirements", func() {
		hwInfo.CPU = &models.CPU{Cpus: 1}
		hwInfo.Memory = []*models.Memory{{Name: "Mem", Total: int64(3 * units.GiB)}}
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		host.HardwareInfo = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			insufficient(hwvalidator.IsSufficient(host))
		}
	})

	It("insufficient_master_but_valid_worker", func() {
		hwInfo.CPU = &models.CPU{Cpus: 8}
		hwInfo.Memory = []*models.Memory{{Name: "Mem", Total: int64(8 * units.GiB)}}
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		host.HardwareInfo = string(hw)
		host.Role = "master"
		insufficient(hwvalidator.IsSufficient(host))
		host.Role = "worker"
		sufficient(hwvalidator.IsSufficient(host))
	})

	It("insufficient_number_of_valid_disks", func() {
		hwInfo.BlockDevices = []*models.BlockDevice{
			// Not disk type
			{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "loop0", Size: validDiskSize},
			// Not enough size
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: notValidDiskSize},
			// Removable
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sda", RemovableDevice: 1, Size: validDiskSize},
			// Read-only
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdh", ReadOnly: true, Size: validDiskSize},
			// Filtered Name
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "nvme01fs", Size: validDiskSize},
		}
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())

		host.HardwareInfo = string(hw)
		insufficient(hwvalidator.IsSufficient(host))

		disks, err := hwvalidator.GetHostValidDisks(host)
		Expect(err).To(HaveOccurred())
		Expect(disks).To(BeNil())
	})

	It("validate_disk_list_return_order", func() {
		nvmename := "nvme01fs"
		hwInfo.BlockDevices = []*models.BlockDevice{
			// Not disk type
			{DeviceType: "loop", Fstype: "squashfs", MajorDeviceNumber: 7, MinorDeviceNumber: 0, Mountpoint: "/sysroot", Name: "aaa", ReadOnly: true, RemovableDevice: 1, Size: validDiskSize},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize + 1},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sda", Size: validDiskSize + 100},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdh", Size: validDiskSize},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: nvmename, Size: validDiskSize},
		}
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		host.HardwareInfo = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("sdh"))
		Expect(len(disks)).Should(Equal(3))
		Expect(isBlockDeviceNameInlist(disks, nvmename)).Should(Equal(false))
	})

	It("invalid_hw_info", func() {
		host.HardwareInfo = "not a valid json"
		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			reply, err := hwvalidator.IsSufficient(host)
			Expect(err).To(HaveOccurred())
			Expect(reply).To(BeNil())
		}
		disks, err := hwvalidator.GetHostValidDisks(host)
		Expect(err).To(HaveOccurred())
		Expect(disks).To(BeNil())
	})

})

func sufficient(reply *IsSufficientReply, err error) {
	Expect(err).NotTo(HaveOccurred())
	Expect(reply.IsSufficient).To(BeTrue())
	Expect(reply.Reason).Should(Equal(""))
}

func insufficient(reply *IsSufficientReply, err error) {
	Expect(err).NotTo(HaveOccurred())
	Expect(reply.IsSufficient).To(BeFalse())
	Expect(reply.Reason).ShouldNot(Equal(""))
}

func isBlockDeviceNameInlist(blockDevices []*models.BlockDevice, name string) bool {
	for _, blockDevice := range blockDevices {
		// Valid disk: type=disk, not removable, not readonly and size bigger than minimum required
		if blockDevice.Name == name {
			return true
		}
	}
	return false
}
