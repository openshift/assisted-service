package loki

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestLokiOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Loki Operator Suite")
}
