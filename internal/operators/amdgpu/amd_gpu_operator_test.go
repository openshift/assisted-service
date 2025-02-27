package amdgpu

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Operator", func() {
	var (
		ctx      context.Context
		operator *operator
	)

	BeforeEach(func() {
		ctx = context.Background()
		operator = NewAMDGPUOperator(common.GetTestLog())
	})

	DescribeTable(
		"Validate hosts",
		func(inventory *models.Inventory, expected api.ValidationResult) {
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
			actual, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"With GPU and secure boot enabled",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: amdVendorID,
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateEnabled,
				},
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Secure boot is enabled, but it needs to be disabled in order to load AMD " +
						"GPU drivers",
				},
			},
		),
		Entry(
			"With GPU and secure boot disabled",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: amdVendorID,
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateDisabled,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"With GPU and secure boot not supported",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: amdVendorID,
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateNotSupported,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"With GPU and secure boot state unknown",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: amdVendorID,
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateUnknown,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Without GPU and secure boot enabled",
			&models.Inventory{
				Gpus: nil,
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateEnabled,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Without GPU and secure boot disabled",
			&models.Inventory{
				Gpus: nil,
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateDisabled,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Without GPU and secure boot not supported",
			&models.Inventory{
				Gpus: nil,
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateNotSupported,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Without GPU and secure boot state unknown",
			&models.Inventory{
				Gpus: nil,
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateUnknown,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"With non AMD GPU and secure boot enabled",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: "1af4",
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateEnabled,
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
	)

	DescribeTable(
		"Validate cluster",
		func(inventories []*models.Inventory, expected api.ValidationResult) {
			hosts := make([]*models.Host, len(inventories))
			for i, inventory := range inventories {
				var inventoryJSON string
				if inventory != nil {
					data, err := json.Marshal(inventory)
					Expect(err).ToNot(HaveOccurred())
					inventoryJSON = string(data)
				}
				hosts[i] = &models.Host{
					Inventory: inventoryJSON,
				}
			}
			cluster := &common.Cluster{
				Cluster: models.Cluster{
					Hosts: hosts,
				},
			}
			actual, _ := operator.ValidateCluster(ctx, cluster)
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Fails if there are no hosts",
			[]*models.Inventory{},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"The AMD GPU operator requires at least one supported AMD GPU, but there is " +
						"none in the discovered hosts.",
				},
			},
		),
		Entry(
			"Fails if there are hosts but no inventory",
			[]*models.Inventory{
				nil,
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"The AMD GPU operator requires at least one supported AMD GPU, but there is " +
						"none in the discovered hosts.",
				},
			},
		),
		Entry(
			"Fails if there are hosts but no GPU",
			[]*models.Inventory{
				{},
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"The AMD GPU operator requires at least one supported AMD GPU, but there is " +
						"none in the discovered hosts.",
				},
			},
		),
		Entry(
			"Fails if there are hosts and unsupported GPUs",
			[]*models.Inventory{
				{
					Gpus: []*models.Gpu{{
						VendorID: "1af4",
					}},
				},
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"The AMD GPU operator requires at least one supported AMD GPU, but there is " +
						"none in the discovered hosts.",
				},
			},
		),
		Entry(
			"Succeeds if there are hosts and supported GPUs",
			[]*models.Inventory{
				{
					Gpus: []*models.Gpu{{
						VendorID: amdVendorID,
					}},
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Succeeds if there are hosts and a mix of supported and unsupported GPUs",
			[]*models.Inventory{
				{
					Gpus: []*models.Gpu{
						{
							VendorID: "1af4",
						},
						{
							VendorID: amdVendorID,
						},
					},
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
		Entry(
			"Succeeds if there are two hosts and only one of them has a supported GPU",
			[]*models.Inventory{
				{},
				{
					Gpus: []*models.Gpu{
						{
							VendorID: amdVendorID,
						},
					},
				},
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
				Reasons:      nil,
			},
		),
	)
})
