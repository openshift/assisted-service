package mtv

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMtv(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mtv Suite")
}
