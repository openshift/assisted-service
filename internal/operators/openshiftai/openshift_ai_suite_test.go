package openshiftai

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

func TestOpenShiftAIOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OpenShift AI Operator")
}

// Logger used for tests:
var logger *logrus.Logger

var _ = BeforeSuite(func() {
	// Create a logger that writes to the Ginkgo writer, so that the log messages will be attached to the output of
	// the right test:
	logger = logrus.New()
	logger.SetOutput(GinkgoWriter)
})
