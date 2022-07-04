package sqllite

import (
	"fmt"
	"testing"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestSqlite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SQLlite Suite")
}

var _ = Describe("sqllite", func() {

	var query *MockQuery

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		query = NewMockQuery(ctrl)
	})

	Context("Query operator bundle", func() {
		It("Query kubevirt version", func() {
			query.EXPECT().GetOperatorVersions("kubevirt").Return([]string{"2.6.5", "2.6.6"}, nil)
			versions, err := query.GetOperatorVersions("kubevirt")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(versions).Should(ContainElement("2.6.5"))
		})

		It("Query with error", func() {
			query.EXPECT().GetOperatorVersions("invalid").Return(nil, fmt.Errorf("The error"))
			versions, err := query.GetOperatorVersions("invalid")
			Expect(err).Should(HaveOccurred())
			Expect(versions).To(BeNil())
		})
	})
})
