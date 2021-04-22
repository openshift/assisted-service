package cluster_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/dbc"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	dbc.InitializeDBTest()
	defer dbc.TerminateDBTest()
	RunSpecs(t, "cluster tests")
}
