package selfnoderemediation

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeFeatureDiscoveryOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Self Node Remediation operator")
}
