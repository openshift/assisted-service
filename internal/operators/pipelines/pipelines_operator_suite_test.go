package pipelines

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestPipelinesOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pipelines")
}
