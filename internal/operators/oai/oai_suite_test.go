package oai

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOAIOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OpenShift AI Operator")
}
