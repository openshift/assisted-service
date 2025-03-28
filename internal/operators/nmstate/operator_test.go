package nmstate

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("NMState Operator", func() {
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
})
