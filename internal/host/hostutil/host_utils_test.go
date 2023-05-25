package hostutil

import (
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

	It("Should not allow hostnames longer than 64 characters", func() {
		for _, hostName := range []string{
			"foobar.local.arbitrary.hostname.longer.than.64-characters.inthis.name",
			"foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234-foobar1234",
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

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "HostUtil Tests")
}
