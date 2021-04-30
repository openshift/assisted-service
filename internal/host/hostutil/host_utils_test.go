package hostutil

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("ValidateInstallerArgs", func() {
	It("Parses correctly", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n", "--save-partindex", "1", "--image-url", "https://example.com/image"}
		err := ValidateInstallerArgs(args)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Denies unexpected arguments", func() {
		args := []string{"--not-supported", "value"}
		err := ValidateInstallerArgs(args)
		Expect(err).To(HaveOccurred())
	})

	It("Succeeds with an empty list", func() {
		err := ValidateInstallerArgs([]string{})
		Expect(err).NotTo(HaveOccurred())
	})
})

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

var _ = Describe("host IP address families", func() {
	It("host doesn't have interfaces", func() {
		host := &models.Host{
			Inventory: "{}",
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeFalse())
	})
	It("error parsing inventory", func() {
		host := &models.Host{
			Inventory: "",
		}
		_, _, err := GetAddressFamilies(host)
		Expect(err).Should(HaveOccurred())
	})
	It("host has only IPv4 addresses", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv6_addresses":[],
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeFalse())
	})
	It("host has only IPv6 addresses", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":
				[
					{
						"ipv6_addresses":[
							"2002:db8::2/64"
						],
						"ipv4_addresses":[]
					}
				]
			}`,
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on same interface", func() {
		host := &models.Host{
			Inventory: `{"interfaces":
				[
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						],
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on different interfaces", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					},
					{
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on different interfaces, reverse order", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					},
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
})

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HostUtil Tests")
}
