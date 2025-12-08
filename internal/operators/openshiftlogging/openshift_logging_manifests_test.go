package openshiftlogging

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v3"
)

var _ = Describe("OpenShift Logging manifest generation", func() {
	var (
		cluster  *common.Cluster
		operator *operator
	)

	BeforeEach(func() {
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.17.0",
			},
		}
		operator = NewOpenShiftLoggingOperator(common.GetTestLog())
	})

	It("Generates the required manifests", func() {
		manifests, customManifest, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveLen(3))
		Expect(manifests).To(HaveKey("50_openshift-logging_ns.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-logging_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-logging_operator_group.yaml"))
		Expect(customManifest).To(BeNil())
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

		nsManifest := manifests["50_openshift-logging_ns.yaml"]
		var nsData map[string]interface{}
		err = yaml.Unmarshal(nsManifest, &nsData)
		Expect(err).ToNot(HaveOccurred())

		metadata := nsData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(Namespace))

		labels := metadata["labels"].(map[string]interface{})
		Expect(labels["openshift.io/cluster-monitoring"]).To(Equal("true"))
	})

	It("Subscription manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		subManifest := manifests["50_openshift-logging_subscription.yaml"]
		var subData map[string]interface{}
		err = yaml.Unmarshal(subManifest, &subData)
		Expect(err).ToNot(HaveOccurred())

		metadata := subData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(SubscriptionName))
		Expect(metadata["namespace"]).To(Equal(Namespace))

		spec := subData["spec"].(map[string]interface{})
		Expect(spec["channel"]).To(Equal(Channel))
		Expect(spec["name"]).To(Equal(SubscriptionName))
		Expect(spec["source"]).To(Equal(Source))
		Expect(spec["sourceNamespace"]).To(Equal(SourceNamespace))
	})

	It("OperatorGroup manifest has correct content", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())

		ogManifest := manifests["50_openshift-logging_operator_group.yaml"]
		var ogData map[string]interface{}
		err = yaml.Unmarshal(ogManifest, &ogData)
		Expect(err).ToNot(HaveOccurred())

		metadata := ogData["metadata"].(map[string]interface{})
		Expect(metadata["name"]).To(Equal(SubscriptionName))
		Expect(metadata["namespace"]).To(Equal(Namespace))

		spec := ogData["spec"].(map[string]interface{})
		targetNamespaces := spec["targetNamespaces"].([]interface{})
		Expect(targetNamespaces).To(HaveLen(1))
		Expect(targetNamespaces[0]).To(Equal(Namespace))
	})
})
