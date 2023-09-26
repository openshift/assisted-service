package imageservice

import (
	"fmt"
	"net/url"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
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
		u, err := KernelURL(baseURL, version, arch, false)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal("/v3/boot-artifacts/kernel"))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))

		u, err = KernelURL(baseURL, version, arch, true)
		Expect(err).NotTo(HaveOccurred())

		parsed, err = url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("http"))
	})

	It("builds a rootfs URL", func() {
		u, err := RootFSURL(baseURL, version, arch, false)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal("/v3/boot-artifacts/rootfs"))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))

		u, err = RootFSURL(baseURL, version, arch, true)
		Expect(err).NotTo(HaveOccurred())

		parsed, err = url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("http"))
	})

	It("builds an initrd URL", func() {
		u, err := InitrdURL(baseURL, id, version, arch, false)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal(fmt.Sprintf("/v3/images/%s/pxe-initrd", id)))
		Expect(parsed.Query().Get("version")).To(Equal(version))
		Expect(parsed.Query().Get("arch")).To(Equal(arch))

		u, err = InitrdURL(baseURL, id, version, arch, true)
		Expect(err).NotTo(HaveOccurred())

		parsed, err = url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("http"))
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

	It("builds an image short URL", func() {
		u, err := ShortImageURL(baseURL, ByAPIKeyPath, id, version, arch, isoType)
		Expect(err).NotTo(HaveOccurred())

		parsed, err := url.Parse(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("https"))
		Expect(parsed.Host).To(Equal("image-service.example.com"))
		Expect(parsed.Path).To(Equal(fmt.Sprintf("/v3/byapikey/%s/%s/%s/full.iso", id, version, arch)))
	})

	It("successfully builds all boot artifact URLs", func() {
		osImage := models.OsImage{CPUArchitecture: &arch, OpenshiftVersion: &version}
		bootArtifacts, err := GetBootArtifactURLs(baseURL, id, &osImage, false)
		Expect(err).To(BeNil())

		scheme := "https"
		host := "image-service.example.com"
		checkURL(bootArtifacts.KernelURL, scheme, host, "/v3/boot-artifacts/kernel", version, arch)
		checkURL(bootArtifacts.RootFSURL, scheme, host, "/v3/boot-artifacts/rootfs", version, arch)
		checkURL(bootArtifacts.InitrdURL, scheme, host, fmt.Sprintf("/v3/images/%s/pxe-initrd", id), version, arch)
	})
})

var _ = Describe("URL parsing", func() {
	var (
		baseURL = "https://image-service.example.com/v3"
		version = "4.10"
		arch    = "x86_64"
		isoType = "full-iso"
		id      = "2bf4b68e-c6cd-4603-9d79-1d05e5aa0665"
	)

	It("successfully parse /images/ URLs", func() {
		u, err := ImageURL(baseURL, id, version, arch, isoType)
		Expect(err).NotTo(HaveOccurred())

		parsedISOType, parsedArch, parsedVersion, err := ParseDownloadURL(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsedISOType).To(Equal(isoType))
		Expect(parsedArch).To(Equal(arch))
		Expect(parsedVersion).To(Equal(version))
	})

	It("successfully parse shorter URLs", func() {
		u, err := ShortImageURL(baseURL, ByAPIKeyPath, id, version, arch, isoType)
		Expect(err).NotTo(HaveOccurred())

		parsedISOType, parsedArch, parsedVersion, err := ParseDownloadURL(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsedISOType).To(Equal(isoType))
		Expect(parsedArch).To(Equal(arch))
		Expect(parsedVersion).To(Equal(version))
	})

	It("successfully parse malformed URL", func() {
		_, _, _, err := ParseDownloadURL("http://host/malformed/path/")
		Expect(err).NotTo(HaveOccurred())
	})
})

func checkURL(u, scheme, host, path, version, arch string) {
	parsed, err := url.Parse(u)
	Expect(err).To(BeNil())
	Expect(parsed.Scheme).To(Equal(scheme))
	Expect(parsed.Host).To(Equal(host))
	Expect(parsed.Path).To(Equal(path))
	Expect(parsed.Query().Get("version")).To(Equal(version))
	Expect(parsed.Query().Get("arch")).To(Equal(arch))
}
