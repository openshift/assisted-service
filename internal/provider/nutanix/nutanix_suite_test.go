package nutanix

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNutanix(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "nutanix tests")
}
