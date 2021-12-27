package vsphere

import (
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("base", func() {
	var log = common.GetTestLog()
	Context("is host supported", func() {
		var provider provider.Provider
		var host *models.Host
		BeforeEach(func() {
			provider = NewVsphereProvider(log)
			host = &models.Host{}
		})

		setHostInventory := func(inventory *models.Inventory, host *models.Host) {
			data, err := json.Marshal(inventory)
			Expect(err).To(BeNil())
			host.Inventory = string(data)
		}

		It("supported", func() {
			inventory := &models.Inventory{
				SystemVendor: &models.SystemVendor{
					Manufacturer: VmwareManufacturer,
				},
			}
			setHostInventory(inventory, host)
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(BeNil())
			Expect(supported).To(BeTrue())
		})

		It("not supported", func() {
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

		It("no inventory", func() {
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(BeNil())
			Expect(supported).To(BeFalse())
		})

		It("invalid inventory", func() {
			host.Inventory = "invalid-inventory"
			supported, err := provider.IsHostSupported(host)
			Expect(err).To(HaveOccurred())
			Expect(supported).To(BeFalse())
		})
	})
})
