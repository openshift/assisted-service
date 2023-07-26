package metallb

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MetalLB manifest generation", func() {
	operator := NewMetalLBOperator(common.GetTestLog(), nil)
	var cluster common.Cluster

	BeforeEach(func() {
		cluster = common.Cluster{Cluster: models.Cluster{}}
	})

	It("valid properties", func() {
		properties := Properties{
			ApiIP:     "192.168.122.201",
			IngressIP: "192.168.122.202",
		}
		propertiesBytes, err := json.Marshal(&properties)
		Expect(err).NotTo(HaveOccurred())
		propertiesStr := string(propertiesBytes)

		metallbConfig := models.MonitoredOperator{
			Name:       Operator.Name,
			Properties: propertiesStr,
		}

		cluster.MonitoredOperators = append(cluster.MonitoredOperators, &metallbConfig)

		openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftManifests).To(HaveLen(3))
		Expect(openshiftManifests["50_openshift-metallb_ns.yaml"]).NotTo(HaveLen(0))
		Expect(openshiftManifests["50_openshift-metallb_subscription.yaml"]).NotTo(HaveLen(0))
		Expect(openshiftManifests["50_openshift-metallb_operator_group.yaml"]).NotTo(HaveLen(0))
		Expect(manifest).NotTo(HaveLen(0))

		for _, openshiftManifest := range openshiftManifests {
			_, err = yaml.YAMLToJSON(openshiftManifest)
			Expect(err).ShouldNot(HaveOccurred())
		}

		_, err = yaml.YAMLToJSON(manifest)
		Expect(err).NotTo(HaveOccurred(), "yamltojson err: %v", err)

		Expect(strings.Contains(string(manifest), properties.ApiIP)).To(BeTrue())
		Expect(strings.Contains(string(manifest), properties.IngressIP)).To(BeTrue())
	})

	It("empty properties", func() {
		_, _, err := operator.GenerateManifests(&cluster)
		Expect(err).To(HaveOccurred())
	})

	It("invalid properites - bad json", func() {
		metallbConfig := models.MonitoredOperator{
			Name:       Operator.Name,
			Properties: "not json",
		}

		cluster.MonitoredOperators = append(cluster.MonitoredOperators, &metallbConfig)

		_, _, err := operator.GenerateManifests(&cluster)
		Expect(err).To(HaveOccurred())
	})

	It("invalid properites - bad cidr", func() {
		properties := Properties{
			ApiIP:     "192.168.122",
			IngressIP: "192.168.122.202",
		}
		propertiesBytes, err := json.Marshal(&properties)
		Expect(err).NotTo(HaveOccurred())
		propertiesStr := string(propertiesBytes)

		metallbConfig := models.MonitoredOperator{
			Name:       Operator.Name,
			Properties: propertiesStr,
		}

		cluster.MonitoredOperators = append(cluster.MonitoredOperators, &metallbConfig)

		_, _, err = operator.GenerateManifests(&cluster)
		Expect(err).To(HaveOccurred())
	})
})
