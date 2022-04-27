package s3wrapper

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Util")
}

var _ = Describe("FixEndpointURL", func() {
	It("returns the original string with a valid http URL", func() {
		endpoint := "http://example.com/stuff"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("http://example.com/stuff"))
	})

	It("returns the original string with a valid https URL", func() {
		endpoint := "https://example.com/stuff"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("https://example.com/stuff"))
	})

	It("prefixes an invalid endpoint with http:// when S3_USE_SSL is not set", func() {
		endpoint := "example.com"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("http://example.com"))
	})

	It("prefixes and invalid endpoint with https:// when S3_USE_SSL is \"true\"", func() {
		endpoint := "example.com"
		os.Setenv("S3_USE_SSL", "true")
		defer os.Unsetenv("S3_USE_SSL")
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("https://example.com"))
	})

	It("returns an error when a prefix does not produce a valid URL", func() {
		endpoint := ":example.com"
		result, err := FixEndpointURL(endpoint)
		Expect(result).To(Equal(""))
		Expect(err).To(HaveOccurred())
	})
})
