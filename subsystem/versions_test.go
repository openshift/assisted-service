package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func hasArmArchitecture(versions models.OpenshiftVersions) bool {
	hasArmArch := false
	for _, version := range versions {
		for _, arch := range version.CPUArchitectures {
			if common.ARM64CPUArchitecture == arch {
				hasArmArch = true
				break
			}
		}
	}
	return hasArmArch
}

var _ = Describe("[minimal-set]test versions", func() {
	It("get versions list", func() {
		reply, err := userBMClient.Versions.V2ListComponentVersions(context.Background(), &versions.V2ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())

		// service, agent, installer, controller
		Expect(len(reply.GetPayload().Versions)).To(Equal(4))
	})

	It("get openshift versions list", func() {
		reply, err := user2BMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(reply.GetPayload())).To(BeNumerically(">=", 1))
	})

	Context("organization based functionality", func() {
		BeforeEach(func() {
			if !Options.FeatureGate {
				Skip("organization based functionality access is disabled")
			}
		})

		It("Doesn't have ARM CPU capability", func() {
			reply, err := userBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasArmArchitecture(reply.GetPayload())).Should(BeFalse())
		})

		It("Have ARM CPU capability", func() {
			reply, err := user2BMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(), &versions.V2ListSupportedOpenshiftVersionsParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(hasArmArchitecture(reply.GetPayload())).Should(BeTrue())
		})
	})

})
