package operators

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestOperators(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operators Suite")
}
