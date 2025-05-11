package subsystem

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("[minimal-set]test versions", func() {
	It("get versions list", func() {
		reply, err := utils_test.TestContext.UserBMClient.Versions.V2ListComponentVersions(context.Background(), &versions.V2ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())

		// service, agent, installer, controller
		Expect(len(reply.GetPayload().Versions)).To(Equal(4))
	})

	It("get openshift versions list", func() {
		reply, err := utils_test.TestContext.UserBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(reply.GetPayload())).To(BeNumerically(">=", 1))
	})

	Context("organization based functionality", func() {
		BeforeEach(func() {
			if !Options.FeatureGate {
				Skip("organization based functionality access is disabled")
			}
		})

		It("multiarch versions are visible to all users", func() {
			reply, err := utils_test.TestContext.UserBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasMultiarch(reply.GetPayload())).To(BeTrue())

			reply2, err := utils_test.TestContext.User2BMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasMultiarch(reply2.GetPayload())).To(BeTrue())
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
