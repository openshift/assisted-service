package cnv

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("CNV manifest generation", func() {
	operator := NewCNVOperator(common.GetTestLog())
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create CNV Manifest", func() {

		manifests, err := operator.GenerateManifests(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(manifests).To(HaveLen(5))
		Expect(manifests["99_openshift-cnv_ns.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-cnv_operator_group.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-cnv_subscription.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-cnv_crd.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-cnv_hco.yaml"]).NotTo(HaveLen(0))
	})

})
