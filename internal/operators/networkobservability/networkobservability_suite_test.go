package networkobservability

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNetworkObservabilityOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Network Observability Operator Suite")
}

