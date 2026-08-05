package tlsconfig_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestTLSConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "tlsconfig tests")
}
