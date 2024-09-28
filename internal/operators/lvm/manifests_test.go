package lvm

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("LVM manifest generation", func() {
	noneHighAvailabilityMode := models.ClusterHighAvailabilityModeNone
	operator := NewLvmOperator(common.GetTestLog(), nil)
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		cluster := common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion:     openshiftVersion,
			HighAvailabilityMode: &noneHighAvailabilityMode,
		}}
		Expect(common.IsSingleNodeCluster(&cluster)).To(BeTrue())
		return &cluster
	}

	Context("LVM Manifest", func() {
		It("Check YAMLs of LVM in SNO deployment mode", func() {
			cluster = getCluster("4.10.17")
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-lvm_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-lvm_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-lvm_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred(), "yamltojson err: %v", err)
		})
	})
	It("Check Subscription information", func() {
		cluster = getCluster("4.12.0-rc.4")
		subscriptionInfo, err := getSubscriptionInfo(cluster.OpenshiftVersion)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_NAME"]).To(Equal(LvmsSubscriptionName))
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_SPEC_NAME"]).To(Equal(LvmsSubscriptionName))
	})

	It("Check Subscription information", func() {
		cluster = getCluster("4.12.0")
		subscriptionInfo, err := getSubscriptionInfo(cluster.OpenshiftVersion)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_NAME"]).To(Equal(LvmsSubscriptionName))
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_SPEC_NAME"]).To(Equal(LvmsSubscriptionName))
	})

	It("Check Subscription information", func() {
		cluster = getCluster("4.10.17")
		subscriptionInfo, err := getSubscriptionInfo(cluster.OpenshiftVersion)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_NAME"]).To(Equal(LvmoSubscriptionName))
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_SPEC_NAME"]).To(Equal(LvmoSubscriptionName))
	})
	It("Check Subscription information", func() {
		cluster = getCluster("4.11.0")
		subscriptionInfo, err := getSubscriptionInfo(cluster.OpenshiftVersion)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_NAME"]).To(Equal(LvmoSubscriptionName))
		Expect(subscriptionInfo["OPERATOR_SUBSCRIPTION_SPEC_NAME"]).To(Equal(LvmoSubscriptionName))
	})
})
