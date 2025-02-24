package openshiftai

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/authorino"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/operators/pipelines"
	"github.com/openshift/assisted-service/internal/operators/serverless"
	"github.com/openshift/assisted-service/internal/operators/servicemesh"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

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
				VendorID: "10de",
			},
			true,
		),
		Entry(
			"Virtio isn't supported by default",
			nil,
			&models.Gpu{
				VendorID: "1af4",
			},
			false,
		),
		Entry(
			"Virtio is supported if explicitly added",
			map[string]string{
				"OPENSHIFT_AI_SUPPORTED_GPUS": "10de,1af4",
			},
			&models.Gpu{
				VendorID: "1af4",
			},
			true,
		),
		Entry(
			"Order isn't relevant",
			map[string]string{
				"OPENSHIFT_AI_SUPPORTED_GPUS": "1af4,10de",
			},
			&models.Gpu{
				VendorID: "10de",
			},
			true,
		),
		Entry(
			"Case isn't relevant",
			nil,
			&models.Gpu{
				VendorID: "10DE",
			},
			true,
		),
	)

	Describe("Get dependencies", func() {
		const flagEnvVar = "OPENSHIFT_AI_SUPPORT_UI_2_37"

		It("Returns the dependencies when compatibility with old UI is explicitly enabled", func() {
			os.Setenv(flagEnvVar, "true")
			defer os.Unsetenv(flagEnvVar)
			operator := NewOpenShiftAIOperator(common.GetTestLog())
			dependencies, err := operator.GetDependencies(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(dependencies).To(ConsistOf(
				authorino.Operator.Name,
				nvidiagpu.Operator.Name,
				odf.Operator.Name,
				pipelines.Operator.Name,
				serverless.Operator.Name,
				servicemesh.Operator.Name,
			))
		})

		It("Returns the dependencies when compatibility with the old UI isn't explicitly enabled or disabled", func() {
			operator := NewOpenShiftAIOperator(common.GetTestLog())
			dependencies, err := operator.GetDependencies(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(dependencies).To(ConsistOf(
				authorino.Operator.Name,
				nvidiagpu.Operator.Name,
				odf.Operator.Name,
				pipelines.Operator.Name,
				serverless.Operator.Name,
				servicemesh.Operator.Name,
			))
		})

		It("Doesn't return the dependencies when compatibility with the old UI is explicitly disabled", func() {
			os.Setenv(flagEnvVar, "false")
			defer os.Unsetenv(flagEnvVar)
			operator := NewOpenShiftAIOperator(common.GetTestLog())
			dependencies, err := operator.GetDependencies(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(dependencies).To(BeEmpty())
		})

	})
})
