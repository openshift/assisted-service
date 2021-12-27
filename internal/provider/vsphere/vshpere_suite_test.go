package vsphere

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVsphere(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "vsphere tests")
}
