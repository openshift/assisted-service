package nodefeaturediscovery

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeFeatureDiscoveryOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node feature discovery operator")
}
