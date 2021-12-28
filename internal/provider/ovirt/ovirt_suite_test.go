package ovirt

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOvirt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ovirt tests")
}
