package hostutil

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("Installation Disk selection", func() {
	const (
		diskName      = "FirstDisk"
		diskId        = "/dev/disk/by-id/FirstDisk"
		otherDiskName = "SecondDisk"
		otherDiskId   = "/dev/disk/by-path/SecondDiskId"
	)

	for _, test := range []struct {
		testName                   string
		currentInstallationDisk    string
		inventoryDisks             []*models.Disk
		expectedInstallationDiskId string
	}{
		{testName: "No previous installation disk, no disks in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{},
			expectedInstallationDiskId: ""},
		{testName: "No previous installation disk, one disk in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "No previous installation disk, two disks in inventory",
			currentInstallationDisk:    "",
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}, {ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory still contains that disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory still contains that disk, but there's another",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: diskId, Name: diskName}, {ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: diskId},
		{testName: `Previous installation disk is set, new inventory still contains that disk, but there's another
						disk with higher priority`,
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: otherDiskId, Name: otherDiskName}, {ID: diskId, Name: diskName}},
			expectedInstallationDiskId: diskId},
		{testName: "Previous installation disk is set, new inventory doesn't contain any disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{},
			expectedInstallationDiskId: ""},
		{testName: "Previous installation disk is set, new inventory only contains a different disk",
			currentInstallationDisk:    diskId,
			inventoryDisks:             []*models.Disk{{ID: otherDiskId, Name: otherDiskName}},
			expectedInstallationDiskId: otherDiskId},
	} {
		test := test
		It(test.testName, func() {
			selectedDisk := DetermineInstallationDisk(test.inventoryDisks, test.currentInstallationDisk)

			if test.expectedInstallationDiskId == "" {
				Expect(selectedDisk).To(BeNil())
			} else {
				Expect(selectedDisk.ID).To(Equal(test.expectedInstallationDiskId))
			}
		})
	}
})

var _ = Describe("Validation", func() {
	It("Should not allow forbidden hostnames", func() {
		for _, hostName := range []string{
			"localhost",
			"localhost.localdomain",
			"localhost4",
			"localhost4.localdomain4",
			"localhost6",
			"localhost6.localdomain6",
		} {
			err := ValidateHostname(hostName)
			Expect(err).To(HaveOccurred())
		}
	})

	It("Should allow permitted hostnames", func() {
		for _, hostName := range []string{
			"foobar",
			"foobar.local",
			"arbitrary.hostname",
		} {
			err := ValidateHostname(hostName)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("Should not allow hostnames longer than 63 characters", func() {
		for _, hostName := range []string{
			"foobar.local.arbitrary.hostname.longer.than.64-characters.inthis.name",
			"foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234",
			"this-host.name-iss.exactly-64.characters.long.so.itt-should.fail",
		} {
			err := ValidateHostname(hostName)
			Expect(err).To(HaveOccurred())
		}
	})
})

var _ = Describe("Ignition endpoint URL generation", func() {
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var dbName string
	var id, clusterID, infraEnvID strfmt.UUID

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		apiVipDNSName := "test.com"
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, APIVipDNSName: &apiVipDNSName}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("using GetIgnitionEndpoint function", func() {
		It("for host with custom MachineConfigPoolName", func() {
			Expect(db.Model(&host).Update("MachineConfigPoolName", "chocobomb").Error).ShouldNot(HaveOccurred())

			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://test.com:22624/config/chocobomb"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for cluster with custom IgnitionEndpoint", func() {
			customEndpoint := "https://foo.bar:33735/acme"
			Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())

			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal(customEndpoint + "/worker"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("failing for cluster with wrong IgnitionEndpoint", func() {
			customEndpoint := "https\\://foo.bar:33735/acme"
			Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())

			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal(""))
			Expect(err).Should(HaveOccurred())
		})
		It("for host with master role", func() {
			Expect(db.Model(&host).Update("Role", "master").Error).ShouldNot(HaveOccurred())
			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://test.com:22624/config/master"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with auto-assing role defaults to worker", func() {
			Expect(db.Model(&host).Update("Role", "auto-assign").Error).ShouldNot(HaveOccurred())
			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://test.com:22624/config/worker"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with no customizations", func() {
			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://test.com:22624/config/worker"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with IPv4 API endpoint", func() {
			Expect(db.Model(&cluster).Update("api_vip_dns_name", "10.0.0.1").Error).ShouldNot(HaveOccurred())
			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://10.0.0.1:22624/config/worker"))
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("for host with IPv6 API endpoint", func() {
			Expect(db.Model(&cluster).Update("api_vip_dns_name", "fe80::1").Error).ShouldNot(HaveOccurred())
			url, err := GetIgnitionEndpoint(&cluster, &host)
			Expect(url).Should(Equal("http://[fe80::1]:22624/config/worker"))
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})

var _ = Describe("Validations", func() {
	Context("Role validity", func() {
		It("Day2 host should accept AutoAssign role", func() {
			isDay2Host := true
			Expect(IsRoleValid(models.HostRoleAutoAssign, isDay2Host)).Should(BeTrue())
		})
	})
})

var _ = Describe("Get Disks of Holder", func() {
	holder1 := models.Disk{DriveType: models.DriveTypeMultipath, Name: "dm-0"}
	holder2 := models.Disk{DriveType: models.DriveTypeMultipath, Name: "dm-1"}
	disksOfHolder1 := []models.Disk{{DriveType: models.DriveTypeISCSI, Name: "sda", Holders: "dm-0"}, {DriveType: models.DriveTypeISCSI, Name: "sdb", Holders: "dm-0"}}
	disksOfHolder2 := []models.Disk{{DriveType: models.DriveTypeFC, Name: "sda", Holders: "dm-1"}}
	disks := []*models.Disk{&holder1, &holder2, &disksOfHolder1[0], &disksOfHolder1[1], &disksOfHolder2[0]}

	It("All disks", func() {
		allDisks := GetAllDisksOfHolder(disks, &holder1)
		Expect(len(allDisks)).To(Equal(2))
		Expect(allDisks).Should(ContainElement(&disksOfHolder1[0]))
		Expect(allDisks).Should(ContainElement(&disksOfHolder1[1]))
	})
	It("Filtered by type", func() {
		filteredDisks := GetDisksOfHolderByType(disks, &holder2, models.DriveTypeFC)
		Expect(len(filteredDisks)).To(Equal(1))
		Expect(filteredDisks).Should(ContainElement(&disksOfHolder2[0]))
	})
})

var _ = Describe("GetHostInstallationDisk", func() {
	var (
		hostId    strfmt.UUID
		diskId    = "/dev/disk/by-id/test-disk"
		diskName  = "test-disk"
		validHost *models.Host
	)

	BeforeEach(func() {
		hostId = strfmt.UUID(uuid.New().String())
	})

	It("should return installation disk when found by disk ID", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).NotTo(HaveOccurred())
		Expect(disk).NotTo(BeNil())
		Expect(disk.ID).To(Equal(diskId))
		Expect(disk.Name).To(Equal(diskName))
	})

	It("should return installation disk when found by disk path", func() {
		expectedFullName := fmt.Sprintf("/dev/%s", diskName)
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, Path: "/dev/sda"},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: expectedFullName,
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).NotTo(HaveOccurred())
		Expect(disk).NotTo(BeNil())
		Expect(disk.ID).To(Equal(diskId))
		Expect(disk.Name).To(Equal(diskName))
	})

	It("should return error when installation disk is not found - empty installation path", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when installation disk is not found - non-matching disk ID", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "/dev/disk/by-id/non-existent-disk",
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when installation disk is not found - non-matching disk path", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{
				{ID: diskId, Name: diskName, Path: "/dev/sda"},
			},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   "",
			InstallationDiskPath: "/dev/non-existent-disk",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when inventory has no disks", func() {
		inventory := &models.Inventory{
			Disks: []*models.Disk{},
		}
		inventoryBytes, _ := json.Marshal(inventory)
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            string(inventoryBytes),
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("installation disk not found for host %s", hostId))))
		Expect(disk).To(BeNil())
	})

	It("should return error when inventory is invalid JSON", func() {
		validHost = &models.Host{
			ID:                   &hostId,
			Inventory:            "invalid json",
			InstallationDiskID:   diskId,
			InstallationDiskPath: "",
		}

		disk, err := GetHostInstallationDisk(validHost)
		Expect(err).To(HaveOccurred())
		Expect(disk).To(BeNil())
	})
})

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "HostUtil Tests")
}
