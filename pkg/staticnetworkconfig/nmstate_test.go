package staticnetworkconfig_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/nmstate/nmstate/rust/src/go/nmstate/v2"
)

var _ = Describe("Nmstate", func() {
	It("Has the GenerateStateFromPolicy function", func() {
		object := nmstate.New()
		Expect(object).ToNot(BeNil())
		result, err := object.GenerateStateFromPolicy("", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("interfaces: []\n"))
	})
})
