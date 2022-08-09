package odf_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

func TestHandler(t *testing.T) {
	common.InitializeTestDatabase()
	defer common.TerminateTestDatabase()

	RegisterFailHandler(Fail)
	RunSpecs(t, "ODF suite")
}
