package fenceagentsremediation

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestFenceAgentsRemediationOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fence Agents Remediation")
}
