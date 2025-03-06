package amdgpu

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestAMDGPUOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AMD GPU")
}
