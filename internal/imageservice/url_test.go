package imageservice

import (
	"fmt"
	"net/url"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestImageService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "imageservice tests")
}

var _ = Describe("URL building", func() {
	var (
		baseURL = "https://image-service.example.com/v3"
		version = "4.10"
		arch    = "x86_64"
		isoType = "full-iso"
		id      = "2bf4b68e-c6cd-4603-9d79-1d05e5aa0665"
	)

	It("builds a kernel URL", func() {
		u, err := KernelURL(baseURL, version, arch)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal("/v3/boot-artifacts/kernel"))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))
	})

	It("builds a rootfs URL", func() {
		u, err := RootFSURL(baseURL, version, arch)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal("/v3/boot-artifacts/rootfs"))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))
	})

	It("builds an initrd URL", func() {
		u, err := InitrdURL(baseURL, id, version, arch)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal(fmt.Sprintf("/v3/images/%s/pxe-initrd", id)))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))
	})

	It("builds an image URL", func() {
		u, err := ImageURL(baseURL, id, version, arch, isoType)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal(fmt.Sprintf("/v3/images/%s", id)))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))
		Expect(parsed.Query().Get("type")).To(Equal(isoType))
	})
})
