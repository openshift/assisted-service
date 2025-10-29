package auth

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "auth tests")
}

var _ = BeforeSuite(func() {
	common.InitializeDBTest()
})

var _ = AfterSuite(func() {
	common.TerminateDBTest()
})
