package oadp

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOadpOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OADP Operator")
}
