package handler

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestHandler(t *testing.T) {
	common.InitializeTestDatabase()
	defer common.TerminateTestDatabase()

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Operators handler Suite")
}
