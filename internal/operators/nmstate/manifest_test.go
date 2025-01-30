package nmstate

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Nmstate manifest generation", func() {
	operator := NewNmstateOperator(common.GetTestLog())
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		return &common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: openshiftVersion,
		}}
	}

	Context("Nmstate manifest", func() {
		It("Check YAMLs of Nmstate", func() {
			cluster = getCluster("4.17.0")
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-nmstate_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-nmstate_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-nmstate_operator_group.yaml"]).NotTo(HaveLen(0))
			Expect(manifest).NotTo(HaveLen(0))

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
			opData, err := getOperatorGroup(Namespace, GroupName)
			Expect(err).To(BeNil())

			Expect(extractData(opData, "metadata.namespace")).To(Equal(Namespace))
		})

	})
})

// extractData extracts data from a manifest data based on a given path.
//
// manifest is the content of the manifest data in yaml format.
// path is the path to the data to extract, formatted as a dot-separated string.
//
// For example, given the following manifest:
//
// name: example
// properties:
//
//	name: property01
//	subproperty:
//	  name: property02
//
// If you call extractData with manifest and "properties.subproperty.name", it will return "property02".
// If any part of the path is invalid or incomplete, extractData will return an empty string.
func extractData(manifest []byte, path string) string {
	keys := strings.Split(path, ".")

	var data interface{}
	err := yaml.Unmarshal(manifest, &data)
	Expect(err).ShouldNot(HaveOccurred())

	ns := data.(map[string]interface{})
	i := 0
	for {
		// the last key should be the node with the actual data to be extracted, so stop
		// traversing the path and return the value at the end of the function instead
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

	result, ok := ns[keys[i]].(string)
	if !ok {
		return ""
	}
	return result
}
