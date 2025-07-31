package metallb_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMetalLB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MetalLB Suite")
}
