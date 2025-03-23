package nodehealthcheck

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeHealthcheckOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Healthcheck Operator")
}
