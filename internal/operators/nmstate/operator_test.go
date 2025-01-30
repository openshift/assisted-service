package nmstate

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

var _ = Describe("NMState Operator", func() {
	const (
		// TODO: change to 0.3 when float values would be accepted for ClusterHostRequirementsDetails.CPUCores
		minCpu    = 0
		minRamMib = 100
	)

	var (
		log      = common.GetTestLog()
		operator api.Operator
		bundle   = []string{"virtualization"}
	)

	Context("operator", func() {
		BeforeEach(func() {
			operator = NewNmstateOperator(log)
		})

		It("should return the right validations ids", func() {
			Expect(operator.GetClusterValidationID()).To(Equal(string(models.ClusterValidationIDNmstateRequirementsSatisfied)))
			Expect(operator.GetHostValidationID()).To(Equal(string(models.HostValidationIDNmstateRequirementsSatisfied)))
		})

		It("should return the right feature support id", func() {
			Expect(operator.GetFeatureSupportID()).To(Equal(models.FeatureSupportLevelIDNMSTATE))
		})

		It("should return no dependencies", func() {
			Expect(operator.GetDependencies(&common.Cluster{})).To(HaveLen(0))
		})

		It("should return the right feature support id", func() {
			Expect(operator.GetBundleLabels()).To(Equal(bundle))
		})
	})

	Context("host requirements", func() {
		BeforeEach(func() {
			operator = NewNmstateOperator(log)
		})

		var cluster common.Cluster

		BeforeEach(func() {
			mode := models.ClusterHighAvailabilityModeFull
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &mode},
			}
		})

		DescribeTable("should return minimum requirements", func(role models.HostRole, expectedRequirements *models.ClusterHostRequirementsDetails) {
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

	Context("Host validation", func() {
		BeforeEach(func() {
			operator = NewNmstateOperator(log)
		})

		var cluster common.Cluster

		BeforeEach(func() {
			mode := models.ClusterHighAvailabilityModeFull
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &mode},
			}
		})

		It("should pass when master has enough resources", func() {
			host := models.Host{Role: models.HostRoleMaster, Inventory: getInventory(int64(1024))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Success))
		})

		It("should pass when worker has enough resources", func() {
			host := models.Host{Role: models.HostRoleWorker, Inventory: getInventory(int64(1024))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Success))
		})

		It("should fail when master not enough memory", func() {
			host := models.Host{Role: models.HostRoleMaster, Inventory: getInventory(int64(50))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Failure))
		})

		It("should fail when worker not enough memory", func() {
			host := models.Host{Role: models.HostRoleWorker, Inventory: getInventory(int64(50))}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Failure))
		})

		It("should fail if no master node inventory was provided", func() {
			host := models.Host{Role: models.HostRoleMaster}

			result, err := operator.ValidateHost(context.TODO(), &cluster, &host, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(api.Pending))
		})

		It("should fail if no worker node inventory was provided", func() {
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
