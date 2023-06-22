package external

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestProviderRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProviderExternal test")
}
