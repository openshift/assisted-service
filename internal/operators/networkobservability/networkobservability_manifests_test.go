package networkobservability

import (
	"bytes"

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

		metadata, ok := nsData["metadata"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "Namespace manifest missing or invalid metadata key")
		Expect(metadata["name"]).To(Equal(Namespace))
	})

	It("Subscription manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		subManifest := manifests["50_openshift-network-observability_subscription.yaml"]
		var subData map[string]interface{}
		err = yaml.Unmarshal(subManifest, &subData)
		Expect(err).ToNot(HaveOccurred())

		metadata, ok := subData["metadata"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "Subscription manifest missing or invalid metadata key")
		Expect(metadata["name"]).To(Equal(SubscriptionName))
		Expect(metadata["namespace"]).To(Equal(Namespace))

		spec, ok := subData["spec"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "Subscription manifest missing or invalid spec key")
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

		metadata, ok := ogData["metadata"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "OperatorGroup manifest missing or invalid metadata key")
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

			// Parse the first document from the custom manifest (supports multi-document YAML)
			decoder := yaml.NewDecoder(bytes.NewReader(customManifest))
			var flowCollectorData map[string]interface{}
			err = decoder.Decode(&flowCollectorData)
			Expect(err).ToNot(HaveOccurred())
			Expect(flowCollectorData).ToNot(BeNil(), "FlowCollector manifest should not be nil")

			Expect(flowCollectorData["kind"]).To(Equal("FlowCollector"))
			Expect(flowCollectorData).To(HaveKey("metadata"), "FlowCollector manifest missing metadata key")
			metadata, ok := flowCollectorData["metadata"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector manifest metadata is not a map")
			Expect(metadata["name"]).To(Equal("cluster"))
			Expect(metadata["namespace"]).To(Equal("netobserv"))

			Expect(flowCollectorData).To(HaveKey("spec"), "FlowCollector manifest missing spec key")
			spec, ok := flowCollectorData["spec"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector manifest spec is not a map")
			Expect(spec).To(HaveKey("agent"), "FlowCollector spec missing agent key")
			agent, ok := spec["agent"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector spec agent is not a map")
			Expect(agent).To(HaveKey("ebpf"), "FlowCollector agent missing ebpf key")
			ebpf, ok := agent["ebpf"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector agent ebpf is not a map")
			Expect(ebpf).To(HaveKey("sampling"), "FlowCollector ebpf missing sampling key")
			// Handle YAML numeric types (can be int, int64, or float64)
			samplingValue := ebpf["sampling"]
			switch v := samplingValue.(type) {
			case int, int64:
				Expect(v).To(BeNumerically("==", 100))
			case float64:
				Expect(v).To(BeNumerically("==", 100))
			default:
				Fail("sampling value is not a numeric type")
			}

			Expect(spec).To(HaveKey("loki"), "FlowCollector spec missing loki key")
			loki, ok := spec["loki"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector spec loki is not a map")
			Expect(loki).To(HaveKey("enabled"), "FlowCollector loki missing enabled key")
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

			// Parse the first document from the custom manifest (supports multi-document YAML)
			decoder := yaml.NewDecoder(bytes.NewReader(customManifest))
			var flowCollectorData map[string]interface{}
			err = decoder.Decode(&flowCollectorData)
			Expect(err).ToNot(HaveOccurred())
			Expect(flowCollectorData).ToNot(BeNil(), "FlowCollector manifest should not be nil")

			Expect(flowCollectorData).To(HaveKey("spec"), "FlowCollector manifest missing spec key")
			spec, ok := flowCollectorData["spec"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector manifest spec is not a map")
			Expect(spec).To(HaveKey("agent"), "FlowCollector spec missing agent key")
			agent, ok := spec["agent"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector spec agent is not a map")
			Expect(agent).To(HaveKey("ebpf"), "FlowCollector agent missing ebpf key")
			ebpf, ok := agent["ebpf"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector agent ebpf is not a map")
			Expect(ebpf).To(HaveKey("sampling"), "FlowCollector ebpf missing sampling key")
			// Handle YAML numeric types (can be int, int64, or float64)
			samplingValue := ebpf["sampling"]
			switch v := samplingValue.(type) {
			case int, int64:
				Expect(v).To(BeNumerically("==", 50)) // Default value
			case float64:
				Expect(v).To(BeNumerically("==", 50)) // Default value
			default:
				Fail("sampling value is not a numeric type")
			}

			Expect(spec).To(HaveKey("loki"), "FlowCollector spec missing loki key")
			loki, ok := spec["loki"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "FlowCollector spec loki is not a map")
			Expect(loki).To(HaveKey("enabled"), "FlowCollector loki missing enabled key")
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

		It("Invalid sampling values trigger error handling; ParseProperties rejects sampling <= 0, GenerateManifests logs a warning and uses safe defaults", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{
					Name:       Name,
					Properties: `{"createFlowCollector": true, "sampling": 0}`,
				},
			}

			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveLen(3))
			// Invalid sampling values trigger error handling: ParseProperties rejects sampling <= 0,
			// GenerateManifests logs a warning and then uses safe defaults (createFlowCollector: false).
			// This is recovery behavior, not a feature. So customManifest should be empty or only contain YAML separators.
			Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
		})
	})
})
