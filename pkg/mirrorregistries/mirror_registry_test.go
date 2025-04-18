package mirrorregistries

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMirrorRegistriesConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MirrorRegistries Suite")
}

var (
	expectedExtractList  = []RegistriesConf{{"location1", []string{"mirror_location1"}}, {"location2", []string{"mirror_location2"}}, {"location3", []string{"mirror_location3"}}}
	configWithGarbage    = `?;,!`
	configWithoutMirrors = `unqualified-search-registries = ["registry1", "registry2", "registry3"]`
	configWithMirrors    = `unqualified-search-registries = ["registry1", "registry2", "registry3"]

[[registry]]
location = "location1"
mirror-by-digest-only = true
prefix = "prefix1"

[[registry.mirror]]
location = "mirror_location1"

[[registry]]
location = "location2"
mirror-by-digest-only = true
prefix = "prefix1"

[[registry.mirror]]
location = "mirror_location2"

[[registry]]
location = "location3"
mirror-by-digest-only = true
prefix = "prefix1"

[[registry.mirror]]
location = "mirror_location3"
`
)

var _ = Describe("Generator tests", func() {

	var _ = Describe("extractLocationMirrorDataFromRegistries", func() {
		It("extracts data from registry config with mirrors", func() {
			dataList, err := ExtractLocationMirrorDataFromRegistriesFromToml(configWithMirrors)
			Expect(err).NotTo(HaveOccurred())
			Expect(dataList).Should(Equal(expectedExtractList))
		})

		It("fails to extract data from registry config without mirrors", func() {
			_, err := ExtractLocationMirrorDataFromRegistriesFromToml(configWithoutMirrors)
			Expect(err.Error()).Should(Equal("failed to find registry key in toml tree, registriesConfToml: unqualified-search-registries = [\"registry1\", \"registry2\", \"registry3\"]"))
		})

		It("fails to extract data from registry config with garbage", func() {
			_, err := ExtractLocationMirrorDataFromRegistriesFromToml(configWithGarbage)
			Expect(err).To(HaveOccurred())
		})

		It("fails to extract data from empty config", func() {
			_, err := ExtractLocationMirrorDataFromRegistriesFromToml("")
			Expect(err.Error()).Should(Equal("failed to find registry key in toml tree, registriesConfToml: "))
		})

	})

	var _ = Describe("IsMirrorRegistriesConfigured", func() {
		It("returns false when CA and registry.conf don't exist", func() {
			m := mirrorRegistriesConfigBuilder{
				MirrorRegistriesConfigPath:      "/tmp/this-file-for-sure-does-not-exist",
				MirrorRegistriesCertificatePath: "/tmp/this-file-for-sure-does-not-exist",
			}
			res := m.IsMirrorRegistriesConfigured()
			Expect(res).Should(Equal(false))
		})

		Context("with registry and CA temp files", func() {
			var (
				tempRegistriesFile *os.File
				tempCAFile         *os.File
				m                  mirrorRegistriesConfigBuilder
				err                error
			)

			BeforeEach(func() {
				tempRegistriesFile, err = os.CreateTemp(os.TempDir(), "registries.*.conf")
				Expect(err).NotTo(HaveOccurred())

				tempCAFile, err = os.CreateTemp(os.TempDir(), "ca.*.crt")
				Expect(err).NotTo(HaveOccurred())
				_, _ = tempCAFile.WriteString("some random CA certificate")

				m = mirrorRegistriesConfigBuilder{
					MirrorRegistriesConfigPath:      tempRegistriesFile.Name(),
					MirrorRegistriesCertificatePath: tempCAFile.Name(),
				}
			})

			AfterEach(func() {
				os.Remove(tempRegistriesFile.Name())
				os.Remove(tempCAFile.Name())
			})

			It("returns false when CA exists and registry.conf has no mirrors", func() {
				_, _ = tempRegistriesFile.WriteString(configWithoutMirrors)

				res := m.IsMirrorRegistriesConfigured()
				Expect(res).Should(Equal(false))
			})

			It("returns false when CA exists and registry.conf has garbage", func() {
				_, _ = tempRegistriesFile.WriteString(configWithGarbage)

				res := m.IsMirrorRegistriesConfigured()
				Expect(res).Should(Equal(false))
			})

			It("returns true when CA exists and registry.conf has mirrors", func() {
				_, _ = tempRegistriesFile.WriteString(configWithMirrors)

				res := m.IsMirrorRegistriesConfigured()
				Expect(res).Should(Equal(true))
			})
		})
	})

	var _ = Describe("GetMirrorCA", func() {
		var (
			tempRegistryCA *os.File
			tempSystemCA   *os.File
			m              mirrorRegistriesConfigBuilder
			err            error
		)

		BeforeEach(func() {
			tempRegistryCA, err = os.CreateTemp(os.TempDir(), "user-registry-ca-bundle.pem")
			Expect(err).NotTo(HaveOccurred())
			_, _ = tempRegistryCA.WriteString("registry CA bundle")

			tempSystemCA, err = os.CreateTemp(os.TempDir(), "tls-ca-bundle.pem")
			Expect(err).NotTo(HaveOccurred())
			_, _ = tempSystemCA.WriteString("system CA bundle")
		})

		It("should return registry bundle when registry bundle exists", func() {
			m = mirrorRegistriesConfigBuilder{
				MirrorRegistriesCertificatePath: tempRegistryCA.Name(),
			}

			result, err := m.GetMirrorCA()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).To(Equal([]byte("registry CA bundle")))
		})

		It("should return the system CA bundle when registry bundle doesn't exist", func() {
			m = mirrorRegistriesConfigBuilder{
				MirrorRegistriesCertificatePath: "/tmp/does-not-exist",
				SystemCertificateBundlePath:     tempSystemCA.Name(),
			}

			result, err := m.GetMirrorCA()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).To(Equal([]byte("system CA bundle")))
		})

		It("should return an error if both files don't exist", func() {
			m = mirrorRegistriesConfigBuilder{
				MirrorRegistriesCertificatePath: "/tmp/does-not-exist",
				SystemCertificateBundlePath:     "/tmp/does-not-exist",
			}

			result, err := m.GetMirrorCA()
			Expect(err).Should(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})
})

const (
	certificate    = "    -----BEGIN CERTIFICATE-----\n    certificate contents\n    -----END CERTIFICATE------"
	sourceRegistry = "quay.io"
	mirrorRegistry = "example-user-registry.com"
)

func getSecureRegistryToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
`,
		sourceRegistry,
		mirrorRegistry,
	)
}

func getSecureRegistryTagOnlyToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
pull-from-mirror = "tag-only"
`,
		sourceRegistry,
		mirrorRegistry,
	)
}

func getInsecureRegistryToml() string {
	x := fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
insecure = true
`,
		sourceRegistry,
		mirrorRegistry,
	)
	return x
}

func getInvalidRegistryToml() string {
	// location has no quotes, will be parsed as double
	return `
[[registry]]
	location = "%s"

	[[registry.mirror]]
	location = 192.168.1.1:5000
	insecure = true
`
}

var _ = Describe("Per cluster mirror registry tests", func() {

	Context("success", func() {
		It("successfully get ImageDigestMirrorSet", func() {
			idmsMirrors, itmsMirrors, insecureRegistries, err := GetImageRegistries(getSecureRegistryToml())
			Expect(err).NotTo(HaveOccurred())
			Expect(len(itmsMirrors)).Should(Equal(0))
			Expect(len(idmsMirrors)).Should(Equal(1))
			Expect(len(insecureRegistries)).Should(Equal(0))

			Expect(len(idmsMirrors[0].Mirrors)).Should(Equal(1))
			Expect(string(idmsMirrors[0].Mirrors[0])).Should(Equal(mirrorRegistry))
			Expect(idmsMirrors[0].Source).Should(Equal(sourceRegistry))
		})
		It("successfully get ImageTagMirrorSet", func() {
			idmsMirrors, itmsMirrors, insecureRegistries, err := GetImageRegistries(getSecureRegistryTagOnlyToml())
			Expect(err).NotTo(HaveOccurred())
			Expect(len(itmsMirrors)).Should(Equal(1))
			Expect(len(idmsMirrors)).Should(Equal(0))
			Expect(len(insecureRegistries)).Should(Equal(0))

			Expect(len(itmsMirrors[0].Mirrors)).Should(Equal(1))
			Expect(string(itmsMirrors[0].Mirrors[0])).Should(Equal(mirrorRegistry))
			Expect(itmsMirrors[0].Source).Should(Equal(sourceRegistry))
		})
		It("successfully get insecure registry", func() {
			idmsMirrors, itmsMirrors, insecureRegistries, err := GetImageRegistries(getInsecureRegistryToml())
			Expect(err).NotTo(HaveOccurred())
			Expect(len(itmsMirrors)).Should(Equal(0))
			Expect(len(idmsMirrors)).Should(Equal(1))
			Expect(len(insecureRegistries)).Should(Equal(1))

			Expect(len(idmsMirrors[0].Mirrors)).Should(Equal(1))
			Expect(string(idmsMirrors[0].Mirrors[0])).Should(Equal(mirrorRegistry))
			Expect(string(idmsMirrors[0].Mirrors[0])).Should(Equal(insecureRegistries[0]))
			Expect(idmsMirrors[0].Source).Should(Equal(sourceRegistry))
		})

	})

	Context("failure", func() {

		When("the user-provided image registry ConfigMap has an invalid toml configuration", func() {
			It("returns parsing error and fails to create configmap", func() {
				idmsMirrors, itmsMirrors, insecureRegistries, err := GetImageRegistries(getInvalidRegistryToml())

				Expect(itmsMirrors).Should(BeNil())
				Expect(idmsMirrors).Should(BeNil())
				Expect(insecureRegistries).Should(BeNil())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(
					"failed to load value of registries.conf into toml tree; incorrectly formatted toml: (6, 13): cannot have two dots in one float"))
			})
		})

		When("the user-provided an empty image registry ConfigMap", func() {
			It("returns parsing error and fails to create configmap", func() {
				idmsMirrors, itmsMirrors, insecureRegistries, err := GetImageRegistries("")

				Expect(itmsMirrors).Should(BeNil())
				Expect(idmsMirrors).Should(BeNil())
				Expect(insecureRegistries).Should(BeNil())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(
					"failed to find registry key in toml tree, registriesConfToml: "))
			})
		})

	})
})
