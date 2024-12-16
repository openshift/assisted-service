package baremetal

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestBaremetalProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Baremetal provider")
}
