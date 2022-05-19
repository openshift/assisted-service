package infraenv_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestInfraEnv(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "infraEnv tests")
}
