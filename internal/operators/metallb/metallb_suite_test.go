package metallb

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMetalLBOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MetalLB Operator")
}
