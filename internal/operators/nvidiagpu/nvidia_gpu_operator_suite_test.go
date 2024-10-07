package nvidiagpu

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNvidiaGPUOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NVIDIA GPU")
}
