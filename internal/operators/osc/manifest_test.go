package osc

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OSC manifest generation", func() {
	operator := NewOscOperator(common.GetTestLog())
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		return &common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: openshiftVersion,
		}}
	}

	Context("OSC manifest", func() {
		It("Check YAMLs of OSC", func() {
			cluster = getCluster("4.17.0")
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-osc_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-osc_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-osc_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("yamltojson err: %v", err))
		})

		It("Check Subscription manifest", func() {
			subscriptionData, err := getSubscription(Namespace, SubscriptionName, Source, SourceName)
			Expect(err).To(BeNil())

			Expect(extractData(subscriptionData, "metadata.name")).To(Equal(SubscriptionName))
			Expect(extractData(subscriptionData, "metadata.namespace")).To(Equal(Namespace))

			Expect(extractData(subscriptionData, "spec.source")).To(Equal(Source))
			Expect(extractData(subscriptionData, "spec.name")).To(Equal(SourceName))
		})

		It("Check namespace manifest", func() {
			nsData, err := getNamespace(Namespace)
			Expect(err).To(BeNil())

			Expect(extractData(nsData, "metadata.name")).To(Equal(Namespace))
		})

		It("Check operator group manifest", func() {
			opData, err := getOperatorGroup(Namespace)
			Expect(err).To(BeNil())

			Expect(extractData(opData, "metadata.namespace")).To(Equal(Namespace))
		})

	})
})

func extractData(manifest []byte, path string) string {
	keys := strings.Split(path, ".")

	var data interface{}
	err := yaml.Unmarshal(manifest, &data)
	Expect(err).ShouldNot(HaveOccurred())

	ns := data.(map[string]interface{})
	i := 0
	for {
		// don't look at the last key which should be a string
		if i > len(keys)-2 {
			break
		}
		subpath, ok := ns[keys[i]]
		if !ok {
			return ""
		}
		newData, ok := subpath.(map[string]interface{})
		if !ok {
			return ""
		}
		ns = newData
		i++
	}

	return ns[keys[i]].(string)
}
