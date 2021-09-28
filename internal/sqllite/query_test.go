package sqllite

import (
	"fmt"
	"testing"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestSqlite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SQLlite Suite")
}

var _ = Describe("sqllite", func() {

	var query *MockOperatorVersionReader

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		query = NewMockOperatorVersionReader(ctrl)
	})

	Context("Query operator bundle", func() {
		It("Query kubevirt version", func() {
			query.EXPECT().GetOperatorVersionsFromDB("index.db", "kubevirt").Return([]string{"2.6.5", "2.6.6"}, nil)
			versions, err := query.GetOperatorVersionsFromDB("index.db", "kubevirt")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(versions).Should(ContainElement("2.6.5"))
		})

		It("Query with error", func() {
			query.EXPECT().GetOperatorVersionsFromDB("index.db", "invalid").Return(nil, fmt.Errorf("the error"))
			versions, err := query.GetOperatorVersionsFromDB("index.db", "invalid")
			Expect(err).Should(HaveOccurred())
			Expect(versions).To(BeNil())
		})

		It("File doesn't exists", func() {
			query.EXPECT().GetOperatorVersionsFromDB("index.db", "invalid").Return(nil, fmt.Errorf("the error"))
			versions, err := query.GetOperatorVersionsFromDB("index.db", "invalid")
			Expect(err).Should(HaveOccurred())
			Expect(versions).To(BeNil())
		})
	})
})
