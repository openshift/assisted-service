package openshiftai

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOpenShiftAIOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OpenShift AI Operator")
}
