package ignition

import (
	"encoding/json"
	"net/url"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIgnition(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ignition Suite")
}

var _ = Describe("GetOSImageURL", func() {
	const testOSImageURL = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123"

	buildIgnitionWithMC := func(osImageURL string) string {
		mcJSON := `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfig","spec":{"osImageURL":"` + osImageURL + `","config":{"ignition":{"version":"3.5.0"}}}}`
		source := "data:," + url.PathEscape(mcJSON)
		ign := map[string]interface{}{
			"ignition": map[string]interface{}{"version": "3.2.0"},
			"storage": map[string]interface{}{
				"files": []map[string]interface{}{
					{
						"path":     "/etc/ignition-machine-config-encapsulated.json",
						"contents": map[string]interface{}{"source": source},
					},
				},
			},
		}
		b, _ := json.Marshal(ign)
		return string(b)
	}

	It("extracts osImageURL from encapsulated MachineConfig", func() {
		result := GetOSImageURL(buildIgnitionWithMC(testOSImageURL))
		Expect(result).To(Equal(testOSImageURL))
	})

	It("returns empty string for ignition without encapsulated MC", func() {
		ign := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
		Expect(GetOSImageURL(ign)).To(BeEmpty())
	})

	It("returns empty string for empty input", func() {
		Expect(GetOSImageURL("")).To(BeEmpty())
	})

	It("returns empty string for invalid JSON", func() {
		Expect(GetOSImageURL("not json")).To(BeEmpty())
	})

	It("returns empty string when osImageURL is empty", func() {
		result := GetOSImageURL(buildIgnitionWithMC(""))
		Expect(result).To(BeEmpty())
	})
})
