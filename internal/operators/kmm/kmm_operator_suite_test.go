package kmm

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestKMMOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KMM")
}
