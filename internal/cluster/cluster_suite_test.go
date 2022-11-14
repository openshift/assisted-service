package cluster_test

import (
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster tests")
}

var _ = BeforeSuite(func() {
	// This is needed to prevent issues with non-initialized time stamp columns when the
	// database is configured for a time zone other than UTC. Without it the result are error
	// like this:
	//
	// ERROR: date/time field value out of range: "0000-12-31T23:45:16.000-00:14" (SQLSTATE 22008)
	strfmt.NormalizeTimeForMarshal = func(t time.Time) time.Time {
		return t.UTC()
	}

	common.InitializeDBTest()
})

var _ = AfterSuite(func() {
	common.TerminateDBTest()
})
