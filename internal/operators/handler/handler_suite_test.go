package handler

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/dbc"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	dbc.InitializeDBTest()
	defer dbc.TerminateDBTest()
	RunSpecs(t, "Operators handler Suite")
}
