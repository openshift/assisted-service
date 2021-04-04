package mirrorregistries

import (

	//"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

func TestMirrorRegistriesConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MirrorRegistriesConfig Suite")
}

var _ = Describe("MirrorRegistriesConfig", func() {

	var (
		mirrorRegistry1        = models.MirrorRegistry{Location: "location1", MirrorLocation: "mirror_location1", Prefix: "prefix1"}
		mirrorRegistry2        = models.MirrorRegistry{Location: "location2", MirrorLocation: "mirror_location2", Prefix: "prefix1"}
		mirrorRegistry3        = models.MirrorRegistry{Location: "location3", MirrorLocation: "mirror_location3", Prefix: "prefix1"}
		mirrorRegistries       = []*models.MirrorRegistry{&mirrorRegistry1, &mirrorRegistry2, &mirrorRegistry3}
		mirrorRegistriesConfig = models.MirrorRegistriesConfig{MirrorRegistries: mirrorRegistries, UnqualifiedSearchRegistries: []string{"registry1", "registry2", "registry3"}}
		registriesConfigInput  = models.MirrorRegistriesCaConfig{CaConfig: "some certificate", MirrorRegistriesConfig: &mirrorRegistriesConfig}
		expectedExtractList    = []RegistriesConf{{"location1", "mirror_location1"}, {"location2", "mirror_location2"}, {"location3", "mirror_location3"}}
		expectedFormatOutput   = `unqualified-search-registries = ["registry1", "registry2", "registry3"]

[[registry]]
  location = "location1"
  mirror-by-digest-only = false
  prefix = "prefix1"

  [[registry.mirror]]
    location = "mirror_location1"

[[registry]]
  location = "location2"
  mirror-by-digest-only = false
  prefix = "prefix1"

  [[registry.mirror]]
    location = "mirror_location2"

[[registry]]
  location = "location3"
  mirror-by-digest-only = false
  prefix = "prefix1"

  [[registry.mirror]]
    location = "mirror_location3"
`
	)

	It("produce mirror registries config", func() {
		registriesFileContent, caConfig, err := FormatRegistriesConfForIgnition(&registriesConfigInput)
		Expect(err).NotTo(HaveOccurred())
		Expect(registriesFileContent).Should(Equal(expectedFormatOutput))
		Expect(caConfig).Should(Equal("some certificate"))
	})

	It("extract data from registries config", func() {
		dataList, err := ExtractLocationMirrorDataFromRegistries(expectedFormatOutput)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataList).Should(Equal(expectedExtractList))
	})
})
