package infraenv_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestInfraEnv(t *testing.T) {
	common.InitializeTestDatabase()
	defer common.TerminateTestDatabase()

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "infraEnv tests")
}
