package openshiftlogging

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOpenShiftLoggingOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OpenShift Logging Operator Suite")
}
