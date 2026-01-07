package networkobservability

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Network Observability manifest generation", func() {
	var (
		cluster  *common.Cluster
		operator *operator
	)

	BeforeEach(func() {
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.12.0",
			},
		}
		operator = NewNetworkObservabilityOperator(common.GetTestLog())
	})

	It("Generates the required manifests", func() {
		manifests, customManifest, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveLen(3))
		Expect(manifests).To(HaveKey("50_openshift-network-observability_ns.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-network-observability_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-network-observability_operator_group.yaml"))
		// When FlowCollector is not created, customManifest may contain only YAML separators
		Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
	})

	It("Generates valid YAML for OpenShift manifests", func() {
		openShiftManifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		for _, openShiftManifest := range openShiftManifests {
			var object interface{}
			err = yaml.Unmarshal(openShiftManifest, &object)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Namespace manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		nsManifest := manifests["50_openshift-network-observability_ns.yaml"]
		var nsData map[string]interface{}
		err = yaml.Unmarshal(nsManifest, &nsData)
		Expect(err).ToNot(HaveOccurred())

		metadata := nsData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(Namespace))
	})

	It("Subscription manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		subManifest := manifests["50_openshift-network-observability_subscription.yaml"]
		var subData map[string]interface{}
		err = yaml.Unmarshal(subManifest, &subData)
		Expect(err).ToNot(HaveOccurred())

		metadata := subData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(SubscriptionName))
		Expect(metadata["namespace"]).To(Equal(Namespace))

		spec := subData["spec"].(map[string]interface{})
		Expect(spec["channel"]).To(Equal("stable"))
		Expect(spec["name"]).To(Equal(SourceName))
		Expect(spec["source"]).To(Equal(Source))
		Expect(spec["sourceNamespace"]).To(Equal("openshift-marketplace"))
	})

	It("OperatorGroup manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		ogManifest := manifests["50_openshift-network-observability_operator_group.yaml"]
		var ogData map[string]interface{}
		err = yaml.Unmarshal(ogManifest, &ogData)
		Expect(err).ToNot(HaveOccurred())

		metadata := ogData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(GroupName))
		Expect(metadata["namespace"]).To(Equal(Namespace))
	})

	Context("FlowCollector generation", func() {
		It("Does not generate FlowCollector when createFlowCollector is false", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: `{"createFlowCollector": false}`,
				},
			}

			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveLen(3))
			// When FlowCollector is not created, customManifest may contain only YAML separators
			Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
		})

		It("Generates FlowCollector when createFlowCollector is true", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: `{"createFlowCollector": true, "sampling": 100}`,
				},
			}

			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveLen(3))
			Expect(customManifest).ToNot(BeEmpty())

			// Parse the custom manifest (it's a multi-document YAML)
			var flowCollectorData map[string]interface{}
			err = yaml.Unmarshal(customManifest, &flowCollectorData)
			Expect(err).ToNot(HaveOccurred())

			Expect(flowCollectorData["kind"]).To(Equal("FlowCollector"))
			metadata := flowCollectorData["metadata"].(map[string]interface{})
			Expect(metadata["name"]).To(Equal("cluster"))
			Expect(metadata["namespace"]).To(Equal("netobserv"))

			spec := flowCollectorData["spec"].(map[string]interface{})
			agent := spec["agent"].(map[string]interface{})
			ebpf := agent["ebpf"].(map[string]interface{})
			Expect(ebpf["sampling"]).To(Equal(100))

			loki := spec["loki"].(map[string]interface{})
			Expect(loki["enabled"]).To(Equal(false))
		})

		It("Uses default values when properties are not provided", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: `{"createFlowCollector": true}`,
				},
			}

			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(customManifest).ToNot(BeEmpty())

			var flowCollectorData map[string]interface{}
			err = yaml.Unmarshal(customManifest, &flowCollectorData)
			Expect(err).ToNot(HaveOccurred())

			spec := flowCollectorData["spec"].(map[string]interface{})
			agent := spec["agent"].(map[string]interface{})
			ebpf := agent["ebpf"].(map[string]interface{})
			Expect(ebpf["sampling"]).To(Equal(50)) // Default value

			loki := spec["loki"].(map[string]interface{})
			Expect(loki["enabled"]).To(Equal(false)) // Always false
		})
	})

	Context("Config parsing", func() {
		It("Handles invalid JSON properties gracefully", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: `{"invalid": json}`,
				},
			}

			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveLen(3))
			// When FlowCollector is not created, customManifest may contain only YAML separators
			Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
		})

		It("Handles empty properties", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: "",
				},
			}

			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveLen(3))
			// When FlowCollector is not created, customManifest may contain only YAML separators
			Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
		})
	})
})
