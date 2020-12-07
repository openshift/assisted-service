package hardware

import (
	"encoding/json"
	"testing"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hardware Validator tests Suite")
}

var _ = Describe("Disk eligibility", func() {
	var (
		hwvalidator   Validator
		testDisk      models.Disk
		bigEnoughSize int64
		tooSmallSize  int64
	)

	BeforeEach(func() {
		var cfg ValidatorCfg
		Expect(envconfig.Process("myapp", &cfg)).ShouldNot(HaveOccurred())
		hwvalidator = NewValidator(logrus.New(), cfg)

		bigEnoughSize = gbToBytes(cfg.MinDiskSizeGb) + 1
		tooSmallSize = gbToBytes(cfg.MinDiskSizeGb) - 1

		// Start off with an eligible default
		testDisk = models.Disk{
			DriveType: "SSD",
			SizeBytes: bigEnoughSize,
		}
	})

	It("Check if HDD is eligible", func() {
		testDisk.DriveType = "SSD"
		eligible, notEligibleReasons := hwvalidator.DiskIsEligible(&testDisk)
		Expect(eligible).To(BeTrue())
		Expect(notEligibleReasons).To(BeEmpty())
	})

	It("Check if SSD is eligible", func() {
		testDisk.DriveType = "HDD"
		eligible, notEligibleReasons := hwvalidator.DiskIsEligible(&testDisk)
		Expect(eligible).To(BeTrue())
		Expect(notEligibleReasons).To(BeEmpty())
	})

	It("Check that ODD is not eligible", func() {
		testDisk.DriveType = "ODD"
		eligible, notEligibleReasons := hwvalidator.DiskIsEligible(&testDisk)
		Expect(eligible).To(BeFalse())
		Expect(notEligibleReasons).ToNot(BeEmpty())
	})

	It("Check that a big enough size is eligible", func() {
		testDisk.SizeBytes = bigEnoughSize
		eligible, notEligibleReasons := hwvalidator.DiskIsEligible(&testDisk)
		Expect(eligible).To(BeTrue())
		Expect(notEligibleReasons).To(BeEmpty())
	})

	It("Check that a small size is not eligible", func() {
		testDisk.SizeBytes = tooSmallSize
		eligible, notEligibleReasons := hwvalidator.DiskIsEligible(&testDisk)
		Expect(eligible).To(BeFalse())
		Expect(notEligibleReasons).ToNot(BeEmpty())
	})
})

var _ = Describe("hardware_validator", func() {
	var (
		hwvalidator         Validator
		host1               *models.Host
		host2               *models.Host
		host3               *models.Host
		inventory           *models.Inventory
		cluster             *common.Cluster
		eligibleEligibility = models.DiskInstallationEligibility{Eligible: true}
		validDiskSize       = int64(128849018880)
		status              = models.HostStatusKnown
	)
	BeforeEach(func() {
		var cfg ValidatorCfg
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
				{DriveType: "ODD", Name: "loop0", InstallationEligibility: &eligibleEligibility},
				{DriveType: "HDD", Name: "sdb", InstallationEligibility: &eligibleEligibility},
			},
		}
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: "1.2.3.0/24",
		}}
		cluster.Hosts = append(cluster.Hosts, host1)
		cluster.Hosts = append(cluster.Hosts, host2)
		cluster.Hosts = append(cluster.Hosts, host3)
	})

	It("validate_disk_list_return_order", func() {
		nvmename := "nvme01fs"
		inventory.Disks = []*models.Disk{
			// Not disk type
			{
				DriveType: "ODD", Name: "aaa", InstallationEligibility: &models.DiskInstallationEligibility{
					Eligible: false,
					NotEligibleReasons: []string{
						"Not HDD/SSD",
					}},
			},
			{DriveType: "SSD", Name: nvmename, InstallationEligibility: &eligibleEligibility, SizeBytes: validDiskSize + 1},
			{DriveType: "SSD", Name: "stam", InstallationEligibility: &eligibleEligibility, SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdb", InstallationEligibility: &eligibleEligibility, SizeBytes: validDiskSize + 2},
			{DriveType: "HDD", Name: "sda", InstallationEligibility: &eligibleEligibility, SizeBytes: validDiskSize + 100},
			{DriveType: "HDD", Name: "sdh", InstallationEligibility: &eligibleEligibility, SizeBytes: validDiskSize + 1},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("sdh"))
		Expect(len(disks)).Should(Equal(5))
		Expect(isBlockDeviceNameInlist(disks, nvmename)).Should(BeTrue())
		Expect(disks[3].DriveType).To(Equal("SSD"))
		Expect(disks[4].DriveType).To(Equal("SSD"))
		Expect(disks[4].Name).To(HavePrefix("nvme"))
	})

	It("validate_aws_disk_detected", func() {
		inventory.Disks = []*models.Disk{
			{
				Name:      "xvda",
				SizeBytes: 128849018880,
				ByPath:    "",
				DriveType: "SSD",
				Hctl:      "",
				Model:     "",
				Path:      "/dev/xvda",
				Serial:    "",
				Vendor:    "",
				Wwn:       "",
				InstallationEligibility: &models.DiskInstallationEligibility{
					Eligible: true,
				},
			},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("xvda"))
		Expect(len(disks)).Should(Equal(1))
	})
})

func isBlockDeviceNameInlist(disks []*models.Disk, name string) bool {
	for _, disk := range disks {
		// Valid disk: type=disk, not removable, not readonly and size bigger than minimum required
		if disk.Name == name {
			return true
		}
	}
	return false
}
