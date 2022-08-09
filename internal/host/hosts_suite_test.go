package host_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestHost(t *testing.T) {
	common.InitializeTestDatabase()
	defer common.TerminateTestDatabase()

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "host state machine tests")
}
