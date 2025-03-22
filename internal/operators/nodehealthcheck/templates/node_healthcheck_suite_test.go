package nodehealthcheck

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeFeatureDiscoveryOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Healthcheck operator")
}
