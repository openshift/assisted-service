package cnv_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("CNV operator", func() {
	var (
		log      = logrus.New()
		operator api.Operator
	)

	BeforeEach(func() {
		cfg := cnv.Config{
			SupportedGPUs: map[string]bool{
				"10de:1db6": true,
				"10de:1eb8": true,
			},
			SupportedSRIOVNetworkIC: map[string]bool{
				"8086:158b": true,
				"15b3:1015": true,
			}}
		operator = cnv.NewCNVOperator(log, cfg, nil)
	})

	Context("host requirements", func() {

		var cluster common.Cluster

		BeforeEach(func() {
			mode := models.ClusterHighAvailabilityModeFull
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &mode},
			}
		})

		table.DescribeTable("should be returned for no inventory", func(role models.HostRole, expectedRequirements *models.ClusterHostRequirementsDetails) {
			host := models.Host{Role: role}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(expectedRequirements))
		},
			table.Entry("for master", models.HostRoleMaster, newRequirements(cnv.MasterCPU, cnv.MasterMemory)),
			table.Entry("for worker", models.HostRoleWorker, newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for worker inventory with supported GPUs",
			func(gpus []*models.Gpu, expectedRequirements *models.ClusterHostRequirementsDetails) {
				host := models.Host{
					Role:      models.HostRoleWorker,
					Inventory: getInventoryWithGPUs(gpus),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(expectedRequirements))
			},
			table.Entry("1 supported GPU",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),
			table.Entry("1 supported GPU+1 unsupported",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1111", VendorID: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),

			table.Entry("2 supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1eb8", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)),
			table.Entry("3 identical supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+3*1024)),

			table.Entry("2 unsupported GPUs only",
				[]*models.Gpu{{DeviceID: "2222", VendorID: "0000"}, {DeviceID: "1111", VendorID: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for worker inventory with supported SR-IOV interfaces",
			func(interfaces []*models.Interface, expectedRequirements *models.ClusterHostRequirementsDetails) {
				host := models.Host{
					Role:      models.HostRoleWorker,
					Inventory: getInventoryWithInterfaces(interfaces),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(expectedRequirements))
			},
			table.Entry("1 supported SR-IOV Interface",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),
			table.Entry("1 supported SR-IOV Interface+1 unsupported",
				[]*models.Interface{{Product: "0x158B", Vendor: "0x8086"}, {Product: "1111", Vendor: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),

			table.Entry("2 supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "1015", Vendor: "15b3"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)),
			table.Entry("3 identical supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+3*1024)),

			table.Entry("2 unsupported SR-IOV Interfaces only",
				[]*models.Interface{{Product: "2222", Vendor: "0000"}, {Product: "1111", Vendor: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for master inventory with GPUs", func(gpus []*models.Gpu) {
			host := models.Host{
				Role:      models.HostRoleMaster,
				Inventory: getInventoryWithGPUs(gpus),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.MasterCPU, cnv.MasterMemory)))
		},
			table.Entry("1 supported GPU",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}}),

			table.Entry("2 supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1eb8", VendorID: "10de"}}),

			table.Entry("3 identical supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}}),

			table.Entry("1 unsupported GPU",
				[]*models.Gpu{{DeviceID: "1111", VendorID: "0000"}}),
			table.Entry("2 unsupported GPUs only",
				[]*models.Gpu{{DeviceID: "2222", VendorID: "0000"}, {DeviceID: "1111", VendorID: "0000"}}),
		)

		table.DescribeTable("should be returned for master inventory with SR-IOV interfaces",
			func(interfaces []*models.Interface) {
				host := models.Host{
					Role:      models.HostRoleMaster,
					Inventory: getInventoryWithInterfaces(interfaces),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.MasterCPU, cnv.MasterMemory)))
			},
			table.Entry("1 supported SR-IOV Interface",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}}),
			table.Entry("1 supported SR-IOV Interface+1 unsupported",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "1111", Vendor: "0000"}}),

			table.Entry("2 supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x1015", Vendor: "0x15b3"}}),
			table.Entry("3 identical supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}}),

			table.Entry("2 unsupported SR-IOV Interfaces only",
				[]*models.Interface{{Product: "2222", Vendor: "0000"}, {Product: "1111", Vendor: "0000"}}),
		)

		It("should be returned for worker with supported GPU and SR-IOV interface", func() {
			host := models.Host{
				Role: models.HostRoleWorker,
				Inventory: getInventoryWith(
					[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}},
					[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}},
				),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)))
		})

		It("should fail for worker with malformed inventory JSON", func() {
			host := models.Host{
				Role:      models.HostRoleWorker,
				Inventory: "garbage...garbage...trash",
			}

			_, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).To(HaveOccurred())
		})

		It("should return reqs for SNO", func() {
			host := models.Host{Role: models.HostRoleMaster}
			haMode := models.ClusterHighAvailabilityModeNone
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &haMode},
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU+cnv.MasterCPU, cnv.WorkerMemory+cnv.MasterMemory)))
		})
	})

	Context("preflight hardware requirements", func() {
		It("should be returned", func() {
			requirements, err := operator.GetPreflightRequirements(context.TODO(), nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements.Dependencies).To(ConsistOf(lso.Operator.Name))
			Expect(requirements.OperatorName).To(BeEquivalentTo(cnv.Operator.Name))

			Expect(requirements.Requirements.Worker.Qualitative).To(HaveLen(3))
			Expect(requirements.Requirements.Worker.Quantitative).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)))

			Expect(requirements.Requirements.Master.Qualitative).To(HaveLen(3))
			Expect(requirements.Requirements.Master.Quantitative).To(BeEquivalentTo(newRequirements(cnv.MasterCPU, cnv.MasterMemory)))

			Expect(requirements.Requirements.Master.Qualitative).To(BeEquivalentTo(requirements.Requirements.Worker.Qualitative))
		})
	})
})

func getInventoryWithGPUs(gpus []*models.Gpu) string {
	inventory := models.Inventory{Gpus: gpus}
	return marshal(inventory)
}

func getInventoryWithInterfaces(interfaces []*models.Interface) string {
	inventory := models.Inventory{Interfaces: interfaces}
	return marshal(inventory)
}

func getInventoryWith(gpus []*models.Gpu, interfaces []*models.Interface) string {
	inventory := models.Inventory{Gpus: gpus, Interfaces: interfaces}
	return marshal(inventory)
}

func marshal(inventory models.Inventory) string {
	inventoryJSON, err := common.MarshalInventory(&inventory)
	Expect(err).ToNot(HaveOccurred())
	return inventoryJSON
}

func newRequirements(cpuCores int64, ramMib int64) *models.ClusterHostRequirementsDetails {
	return &models.ClusterHostRequirementsDetails{CPUCores: cpuCores, RAMMib: ramMib}
}
