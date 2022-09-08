package subsystem

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("[minimal-set]test versions", func() {
	It("get versions list", func() {
		reply, err := userBMClient.Versions.V2ListComponentVersions(context.Background(), &versions.V2ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())

		// service, agent, installer, controller
		Expect(len(reply.GetPayload().Versions)).To(Equal(4))
	})

	It("get openshift versions list", func() {
		reply, err := userBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(reply.GetPayload())).To(BeNumerically(">=", 1))
	})

	Context("organization based functionality", func() {
		BeforeEach(func() {
			if !Options.FeatureGate {
				Skip("organization based functionality access is disabled")
			}
		})
		It("Doesn't have multiarch capability", func() {
			reply, err := userBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasMultiarch(reply.GetPayload())).Should(BeFalse())
		})
		It("Has multiarch capability", func() {
			// (MGMT-11859) This test relies on the fact that multiarch release images have
			//              "-multi" suffix when presented via SupportedOpenshiftVersions API.
			//              As soon as we collapse single- and multiarch releases, the contract
			//              defined by "func hasMultiarch()" will not be valid anymore.
			reply, err := user2BMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasMultiarch(reply.GetPayload())).Should(BeTrue())
		})
	})
})

func hasMultiarch(versions models.OpenshiftVersions) bool {
	hasMultiarch := false
	for _, version := range versions {
		if strings.HasSuffix(*version.DisplayName, "-multi") {
			hasMultiarch = true
			break
		}
	}
	return hasMultiarch
}
