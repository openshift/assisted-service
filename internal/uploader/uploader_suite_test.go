package uploader

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestEventsUploader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Events uploader test Suite")
}
