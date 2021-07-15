package mirrorregistries

import (
	"io/ioutil"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMirrorRegistriesConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MirrorRegistriesConfig Suite")
}

var _ = Describe("MirrorRegistriesConfig", func() {

	var (
		expectedExtractList  = []RegistriesConf{{"location1", "mirror_location1"}, {"location2", "mirror_location2"}, {"location3", "mirror_location3"}}
		expectedFormatOutput = `unqualified-search-registries = ["registry1", "registry2", "registry3"]

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

	It("extract data from registries config", func() {
		dataList, err := extractLocationMirrorDataFromRegistries(expectedFormatOutput)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataList).Should(Equal(expectedExtractList))
	})

	It("test get CA contents", func() {
		file, err := ioutil.TempFile("", "ca.crt")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.WriteString("some ca data")
		Expect(err).NotTo(HaveOccurred())
		contents, err := readFile(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).Should(Equal("some ca data"))
	})
})
