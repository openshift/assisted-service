package nvidiagpu

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
		operator = NewNvidiaGPUOperator(common.GetTestLog())
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
					VendorID: nvidiaVendorID,
				}},
				Boot: &models.Boot{
					SecureBootState: models.SecureBootStateEnabled,
				},
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Secure boot is enabled, but it needs to be disabled in order to load NVIDIA " +
						"GPU drivers",
				},
			},
		),
		Entry(
			"With GPU and secure boot disabled",
			&models.Inventory{
				Gpus: []*models.Gpu{{
					VendorID: nvidiaVendorID,
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
					VendorID: nvidiaVendorID,
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
					VendorID: nvidiaVendorID,
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
			"With non NVIDIA GPU and secure boot enabled",
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
})
