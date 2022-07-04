package vsphere

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestVsphere(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "vsphere tests")
}
