package mtv

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MTV manifest generation", func() {
	operator := NewMTVOperator(common.GetTestLog())
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		cluster := common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: openshiftVersion,
		}}
		return &cluster
	}

	Context("MVP manifest", func() {
		It("Check YAMLs of MTV", func() {
			cluster = getCluster("4.16.0")
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-mtv_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-mtv_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-mtv_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred(), "yamltojson err: %v", err)
		})

		It("Check Subscription manifest", func() {
			subscriptionData, err := getSubscription(Namespace, Subscription, Source, SourceName)
			Expect(err).To(BeNil())

			Expect(extractData(subscriptionData, "metadata.name")).To(Equal(Subscription))
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

		It("Check controller manifest", func() {
			controllerData, err := getController(Namespace)
			Expect(err).To(BeNil())

			Expect(extractData(controllerData, "metadata.namespace")).To(Equal(Namespace))
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
