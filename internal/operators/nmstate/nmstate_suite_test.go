package nmstate

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNmstate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Nmstate Suite")
}
