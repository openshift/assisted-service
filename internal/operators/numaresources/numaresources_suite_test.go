package numaresources

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNumaResourcesOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NUMA Resources Operator")
}
