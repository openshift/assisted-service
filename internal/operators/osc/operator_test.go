package osc

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

var _ = Describe("OSC Operator", func() {
	const (
		minCpu    = 1
		minRamMib = 1024
	)

	var (
		log      = logrus.New()
		operator api.Operator
	)

	Context("operator", func() {
		BeforeEach(func() {
			operator = NewOscOperator(log)
		})

		It("should return the right validations ids", func() {
			Expect(operator.GetClusterValidationID()).To(Equal(string(models.ClusterValidationIDOscRequirementsSatisfied)))
			Expect(operator.GetHostValidationID()).To(Equal(string(models.HostValidationIDOscRequirementsSatisfied)))
		})

		It("should return the right feature support id", func() {
			Expect(operator.GetFeatureSupportID()).To(Equal(models.FeatureSupportLevelIDOSC))
		})
	})

	Context("host requirements", func() {
		BeforeEach(func() {
			operator = NewOscOperator(log)
		})

		var cluster common.Cluster

		BeforeEach(func() {
			cluster = common.Cluster{
				Cluster: models.Cluster{ControlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode},
			}
		})

		DescribeTable("should be returned for no inventory", func(role models.HostRole, expectedRequirements *models.ClusterHostRequirementsDetails) {
			host := models.Host{Role: role}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(expectedRequirements))

		},
			Entry("min requirements", models.HostRoleMaster, newRequirements(minCpu, minRamMib)),
			Entry("min requirements", models.HostRoleWorker, newRequirements(minCpu, minRamMib)),
		)

	})

	Context("Validate host", func() {
		BeforeEach(func() {
			operator = NewOscOperator(log)
		})

		var cluster common.Cluster

		BeforeEach(func() {
			cluster = common.Cluster{
				Cluster: models.Cluster{ControlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode},
			}
		})

		It("master host should be valid", func() {
			host := models.Host{Role: models.HostRoleMaster, Inventory: getInventory(int64(1024))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Success))
		})

		It("worker host should be valid", func() {
			host := models.Host{Role: models.HostRoleWorker, Inventory: getInventory(int64(1024))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Success))
		})

		It("master host should be fail - not enough memory", func() {
			host := models.Host{Role: models.HostRoleMaster, Inventory: getInventory(int64(300))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Failure))
		})

		It("worker host should be fail - not enough memory", func() {
			host := models.Host{Role: models.HostRoleWorker, Inventory: getInventory(int64(300))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Failure))
		})

		It("master host should be fail - no inventory", func() {
			host := models.Host{Role: models.HostRoleMaster}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Pending))
		})

		It("worker host should be fail - no inventory", func() {
			host := models.Host{Role: models.HostRoleWorker}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Pending))
		})

	})
})

func newRequirements(cpuCores int64, ramMib int64) *models.ClusterHostRequirementsDetails {
	return &models.ClusterHostRequirementsDetails{CPUCores: cpuCores, RAMMib: ramMib}
}

func getInventory(memMiB int64) string {
	inventory := models.Inventory{CPU: &models.CPU{Architecture: "x86_64", Count: 1}, Memory: &models.Memory{UsableBytes: conversions.MibToBytes(memMiB)}}
	inventoryJSON, err := common.MarshalInventory(&inventory)
	Expect(err).ToNot(HaveOccurred())
	return inventoryJSON
}
