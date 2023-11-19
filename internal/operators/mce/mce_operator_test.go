package mce

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var _ = Describe("MCE Operator", func() {

	var (
		ctx                           = context.TODO()
		operator                      = NewMceOperator(common.GetTestLog(), EnvironmentalConfig{})
		hostWithNoInventory           = &models.Host{}
		hostWithInsufficientResources = &models.Host{
			Inventory: Inventory(&InventoryResources{
				Cpus: 12,
				Ram:  8 * conversions.GiB,
			}),
		}
		hostWithSufficientResources = &models.Host{
			Inventory: Inventory(&InventoryResources{
				Cpus: 12,
				Ram:  32 * conversions.GiB,
			}),
		}
	)

	Context("GetHostRequirements", func() {
		fullHaMode := models.ClusterHighAvailabilityModeFull
		snoMode := models.ClusterHighAvailabilityModeNone

		table.DescribeTable("get host requirements when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("on a multinode cluster",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &fullHaMode, OpenshiftVersion: "4.13.0", Hosts: []*models.Host{hostWithSufficientResources}}},
				hostWithSufficientResources,
				&models.ClusterHostRequirementsDetails{CPUCores: MinimumCPU, RAMMib: conversions.GibToMib(MinimumMemory)},
			),
			table.Entry("on an SNO cluster",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &snoMode, OpenshiftVersion: "4.13.0", Hosts: []*models.Host{hostWithSufficientResources}}},
				hostWithSufficientResources,
				&models.ClusterHostRequirementsDetails{CPUCores: SNOMinimumCpu, RAMMib: conversions.GibToMib(SNOMinimumMemory)},
			),
		)
	})

	Context("ValidateHost", func() {
		table.DescribeTable("validate host when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("host with no inventory",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{hostWithNoInventory}}},
				hostWithNoInventory,
				api.ValidationResult{Status: api.Pending, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Missing Inventory in the host"}},
			),
			table.Entry("host with insufficient memory",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{hostWithInsufficientResources}}},
				hostWithInsufficientResources,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Insufficient memory to deploy multicluster engine. Required memory is 16384 MiB but found 8192 MiB"}},
			),

			table.Entry("master with sufficient resources",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{hostWithSufficientResources}}},
				hostWithSufficientResources,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
		)
	})
	Context("ValidateCluster", func() {
		mceMinOpenshiftVersion, err := getMinMceOpenshiftVersion(operator.config.OcpMceVersionMap)
		Expect(err).ToNot((HaveOccurred()))

		table.DescribeTable("validate cluster when ", func(cluster *common.Cluster, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateCluster(ctx, cluster)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("Openshift version less than minimal",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{hostWithSufficientResources}, OpenshiftVersion: "4.9.0"}},
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{fmt.Sprintf("multicluster engine is only supported for openshift versions %s and above", *mceMinOpenshiftVersion)}},
			),
			table.Entry("Openshift version more than minimal",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{hostWithSufficientResources}, OpenshiftVersion: *mceMinOpenshiftVersion}},
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
		)
	})
})
