package nodemaintenance

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeMaintenanceOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Maintenance Operator")
}
