package external

import (
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("oci", func() {
	var log = common.GetTestLog()
	Context("host", func() {
		var provider provider.Provider
		var host *models.Host
		BeforeEach(func() {
			provider = NewOciExternalProvider(log)
			host = &models.Host{}
		})

		setHostInventory := func(inventory *models.Inventory, host *models.Host) {
			data, err := json.Marshal(inventory)
			Expect(err).To(BeNil())
			host.Inventory = string(data)
		}

		It("is supported", func() {
			inventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: OCIManufacturer,
				},
			}
			setHostInventory(inventory, host)
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(BeNil())
			Expect(supported).To(BeTrue())
		})

		It("is not supported", func() {
			inventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: "",
				},
			}
			setHostInventory(inventory, host)
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(BeNil())
			Expect(supported).To(BeFalse())
		})

		It("are supported", func() {
			inventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: OCIManufacturer,
				},
			}
			setHostInventory(inventory, host)
			supported, err := provider.AreHostsSupported([]*models.Host{host, host})
			Expect(err).To(BeNil())
			Expect(supported).To(BeTrue())
		})

		It("are not supported", func() {
			inventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: OCIManufacturer,
				},
			}
			setHostInventory(inventory, host)

			notOCIHost := &models.Host{}
			notOCIinventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: "",
				},
			}
			setHostInventory(notOCIinventory, notOCIHost)

			supported, err := provider.AreHostsSupported([]*models.Host{host, notOCIHost})
			Expect(err).To(BeNil())
			Expect(supported).To(BeFalse())
		})

		It("has no inventory", func() {
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(BeNil())
			Expect(supported).To(BeFalse())
		})

		It("has an invalid inventory", func() {
			host.Inventory = "invalid-inventory"
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(HaveOccurred())
			Expect(supported).To(BeFalse())
		})
	})
})
