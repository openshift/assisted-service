package kubedescheduler

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestKubeDeschedulerOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube Descheduler Operator")
}
