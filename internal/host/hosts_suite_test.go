package host_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/dbc"
)

func TestHost(t *testing.T) {
	RegisterFailHandler(Fail)
	dbc.InitializeDBTest()
	defer dbc.TerminateDBTest()
	RunSpecs(t, "host state machine tests")
}
