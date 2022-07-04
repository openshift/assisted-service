package ovirt

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestOvirt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ovirt tests")
}
