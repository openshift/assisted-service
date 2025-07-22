package clusterobservability

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestClusterObservabilityOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Observability Operator")
}
