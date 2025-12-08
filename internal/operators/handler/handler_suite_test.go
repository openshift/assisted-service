package handler

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operators handler Suite")
}

var _ = BeforeSuite(func() {
	common.InitializeDBTest()
})

var _ = AfterSuite(func() {
	common.TerminateDBTest()
})
