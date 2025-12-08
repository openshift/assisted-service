package infraenv_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestInfraEnv(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "infraEnv tests")
}

var _ = BeforeSuite(func() {
	common.InitializeDBTest()
})

var _ = AfterSuite(func() {
	common.TerminateDBTest()
})
