package cnv_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("CNV operator", func() {
	var (
		log      = logrus.New()
		operator api.Operator
	)

	BeforeEach(func() {
		cfg := cnv.Config{SupportedGPUs: map[string]bool{
			"10de:1db6": true,
			"10de:1eb8": true,
		}}
		operator = cnv.NewCNVOperatorWithConfig(log, cfg)
	})

	Context("host requirements", func() {

		table.DescribeTable("should be returned for no inventory", func(role models.HostRole, expectedRequirements *models.ClusterHostRequirementsDetails) {
			host := models.Host{Role: role}

			requirements, err := operator.GetHostRequirements(context.TODO(), nil, &host)

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
					Inventory: getInventory(gpus),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), nil, &host)

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

		table.DescribeTable("should be returned for master inventory with GPUs", func(gpus []*models.Gpu) {
			host := models.Host{
				Role:      models.HostRoleMaster,
				Inventory: getInventory(gpus),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), nil, &host)

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

		It("should fail for worker with malformed inventory JSON", func() {
			host := models.Host{
				Role:      models.HostRoleWorker,
				Inventory: "garbage...garbage...trash",
			}

			_, err := operator.GetHostRequirements(context.TODO(), nil, &host)

			Expect(err).To(HaveOccurred())
		})

		It("should take into account GPUs from env variables", func() {
			cfg := cnv.Config{SupportedGPUs: map[string]bool{
				"0000:1111": true,
				"2222:3333": true,
			}}
			operator = cnv.NewCNVOperatorWithConfig(log, cfg)

			host := models.Host{
				Role:      models.HostRoleWorker,
				Inventory: getInventory([]*models.Gpu{{DeviceID: "1111", VendorID: "0000"}, {DeviceID: "3333", VendorID: "2222"}}),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), nil, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)))
		})
	})
})

func getInventory(gpus []*models.Gpu) string {
	inventory := models.Inventory{Gpus: gpus}
	inventoryJSON, err := hostutil.MarshalInventory(&inventory)
	Expect(err).ToNot(HaveOccurred())
	return inventoryJSON
}

func newRequirements(cpuCores int64, ramMib int64) *models.ClusterHostRequirementsDetails {
	return &models.ClusterHostRequirementsDetails{CPUCores: cpuCores, RAMMib: ramMib}
}
