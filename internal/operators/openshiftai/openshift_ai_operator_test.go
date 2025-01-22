package openshiftai

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

const virtIOVendorID = "1a4f"

var _ = Describe("Operator", func() {
	var (
		ctx      context.Context
		operator *operator
	)

	BeforeEach(func() {
		ctx = context.Background()
		operator = NewOpenShiftAIOperator(common.GetTestLog())
	})

	DescribeTable(
		"Validate hosts",
		func(host *models.Host, expected api.ValidationResult) {
			cluster := &common.Cluster{
				Cluster: models.Cluster{
					Hosts: []*models.Host{
						host,
					},
				},
			}
			actual, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Host with no inventory",
			&models.Host{},
			api.ValidationResult{
				Status:       api.Pending,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Missing inventory in the host",
				},
			},
		),
		Entry(
			"Worker host with insufficient memory",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 8,
					Ram:  8 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Insufficient memory to deploy OpenShift AI, requires 32 GiB but found 8 GiB",
				},
			},
		),
		Entry(
			"Worker host with insufficient CPU",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 4,
					Ram:  32 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Insufficient CPU to deploy OpenShift AI, requires 8 CPU cores but found 4",
				},
			},
		),
		Entry(
			"Worker host with sufficient resources",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 8,
					Ram:  32 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
			},
		),
	)

	DescribeTable(
		"Is supported GPU",
		func(env map[string]string, gpu *models.Gpu, expected bool) {
			// Set the environment variables and restore the previous values when finished:
			for name, value := range env {
				oldValue, present := os.LookupEnv(name)
				if present {
					defer func() {
						err := os.Setenv(name, oldValue)
						Expect(err).ToNot(HaveOccurred())
					}()
				} else {
					defer func() {
						err := os.Unsetenv(name)
						Expect(err).ToNot(HaveOccurred())
					}()
				}
				err := os.Setenv(name, value)
				Expect(err).ToNot(HaveOccurred())
			}

			// Create the operator:
			operator = NewOpenShiftAIOperator(common.GetTestLog())

			// Run the check:
			actual, err := operator.isSupportedGpu(gpu)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"NVIDIA is supported by default",
			nil,
			&models.Gpu{
				VendorID: nvidiagpu.VendorID,
			},
			true,
		),
		Entry(
			"Virtio isn't supported by default",
			nil,
			&models.Gpu{
				VendorID: virtIOVendorID,
			},
			false,
		),
		Entry(
			"VirtIO is supported if explicitly added",
			map[string]string{
				"OPENSHIFT_AI_SUPPORTED_GPUS": nvidiagpu.VendorID + "," + virtIOVendorID,
			},
			&models.Gpu{
				VendorID: virtIOVendorID,
			},
			true,
		),
		Entry(
			"Order isn't relevant",
			map[string]string{
				"OPENSHIFT_AI_SUPPORTED_GPUS": virtIOVendorID + "," + nvidiagpu.VendorID,
			},
			&models.Gpu{
				VendorID: nvidiagpu.VendorID,
			},
			true,
		),
		Entry(
			"Case isn't relevant",
			nil,
			&models.Gpu{
				VendorID: strings.ToUpper(nvidiagpu.VendorID),
			},
			true,
		),
		Entry(
			"NVIDIA is supported even if not explicitly added",
			map[string]string{
				"OPENSHIFT_AI_SUPPORTED_GPUS": virtIOVendorID,
			},
			&models.Gpu{
				VendorID: nvidiagpu.VendorID,
			},
			true,
		),
	)

	It("Adds the NVIDIA GPU operator if there is one host with an NVIDIA GPU", func() {
		inventory := &models.Inventory{
			Gpus: []*models.Gpu{
				{
					VendorID: nvidiagpu.VendorID,
				},
			},
		}
		data, err := json.Marshal(inventory)
		Expect(err).ToNot(HaveOccurred())
		host := &models.Host{
			Inventory: string(data),
		}
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{
					host,
				},
			},
		}
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(ContainElement(nvidiagpu.Operator.Name))
	})

	It("Doesn't add the NVIDIA GPU operator if there are no hosts", func() {
		cluster := &common.Cluster{
			Cluster: models.Cluster{},
		}
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).ToNot(ContainElement(nvidiagpu.Operator.Name))
	})

	It("Doesn't add the NVIDIA GPU operator if there is no inventory", func() {
		host := &models.Host{
			Inventory: "",
		}
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{
					host,
				},
			},
		}
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).ToNot(ContainElement(nvidiagpu.Operator.Name))
	})

	It("Doesn't add the NVIDIA GPU operator if there are hosts but no GPUs", func() {
		inventory := &models.Inventory{
			Gpus: []*models.Gpu{},
		}
		data, err := json.Marshal(inventory)
		Expect(err).ToNot(HaveOccurred())
		host := &models.Host{
			Inventory: string(data),
		}
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{
					host,
				},
			},
		}
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).ToNot(ContainElement(nvidiagpu.Operator.Name))
	})

	It("Doesn't add the NVIDIA GPU operator if there aren't NVIDIA GPUs", func() {
		inventory := &models.Inventory{
			Gpus: []*models.Gpu{
				{
					VendorID: virtIOVendorID,
				},
			},
		}
		data, err := json.Marshal(inventory)
		Expect(err).ToNot(HaveOccurred())
		host := &models.Host{
			Inventory: string(data),
		}
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{
					host,
				},
			},
		}
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).ToNot(ContainElement(nvidiagpu.Operator.Name))
	})
})
