package authorino

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestAuthorinoOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authorino Operator")
}
