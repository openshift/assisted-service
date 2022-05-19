package ignition_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIgnition(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ignition Suite")
}
