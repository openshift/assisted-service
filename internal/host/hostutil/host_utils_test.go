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
})

var _ = Describe("Ignition endpoint URL generation", func() {
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var id, clusterID, infraEnvID strfmt.UUID

	BeforeEach(func() {
		db, _ = common.PrepareTestDB()

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		apiVipDNSName := "test.com"
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, APIVipDNSName: &apiVipDNSName}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
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
	})
})

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HostUtil Tests")
}
