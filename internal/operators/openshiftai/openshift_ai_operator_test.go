package openshiftai

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
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
})
