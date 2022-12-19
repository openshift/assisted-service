package mirrorregistries

import (
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

var _ = Describe("extractLocationMirrorDataFromRegistries", func() {
	It("extracts data from registry config with mirrors", func() {
		dataList, err := extractLocationMirrorDataFromRegistries(configWithMirrors)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataList).Should(Equal(expectedExtractList))
	})

	It("fails to extract data from registry config without mirrors", func() {
		_, err := extractLocationMirrorDataFromRegistries(configWithoutMirrors)
		Expect(err.Error()).Should(Equal("failed to cast registry key to toml Tree, registriesConfToml: unqualified-search-registries = [\"registry1\", \"registry2\", \"registry3\"]"))
	})

	It("fails to extract data from registry config with garbage", func() {
		_, err := extractLocationMirrorDataFromRegistries(configWithGarbage)
		Expect(err).To(HaveOccurred())
	})

	It("fails to extract data from empty config", func() {
		_, err := extractLocationMirrorDataFromRegistries("")
		Expect(err.Error()).Should(Equal("failed to cast registry key to toml Tree, registriesConfToml: "))
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
