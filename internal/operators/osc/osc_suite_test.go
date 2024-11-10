package osc

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOsc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Osc Suite")
}
