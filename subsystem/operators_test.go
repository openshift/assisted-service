package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/operators"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Operators endpoint tests", func() {

	Context("supported-operators", func() {
		It("should return all supported operators", func() {
			reply, err := userBMClient.Operators.ListSupportedOperators(context.TODO(), operators.NewListSupportedOperatorsParams())

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.GetPayload()).To(ConsistOf("ocs", "lso", "cnv"))
		})

		It("should provide operator properties", func() {
			params := operators.NewListOperatorPropertiesParams().WithOperatorName("ocs")
			reply, err := userBMClient.Operators.ListOperatorProperties(context.TODO(), params)

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.Payload).To(BeEquivalentTo(models.OperatorProperties{}))
		})
	})
})
