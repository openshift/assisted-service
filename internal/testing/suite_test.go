package testing

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestTesting(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Testing")
}
