package oadp

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("OADP manifests", func() {
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
		operator = NewOadpOperator(common.GetTestLog())
	})

	It("check that openshift manifests are created", func() {
		openshiftManifests, customManifests, err := operator.GenerateManifests(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftManifests).To(HaveLen(3))
		Expect(openshiftManifests).To(HaveKey("50_oadp_namespace.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_oadp_operatorgroup.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_oadp_subscription.yaml"))
		Expect(customManifests).To(ContainSubstring("oadp"))
	})
})
