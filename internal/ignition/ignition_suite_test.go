package ignition_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestIgnition(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ignition Suite")
}
