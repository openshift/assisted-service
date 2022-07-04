package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/featuresupport"
)

var _ = Describe("V2ListFeatureSupportLevels API", func() {
	It("Should return the feature list", func() {
		response, err := userBMClient.Installer.V2ListFeatureSupportLevels(context.Background(), installer.NewV2ListFeatureSupportLevelsParams())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(response.Payload).To(BeEquivalentTo(featuresupport.SupportLevelsList))
	})
	It("Should respond with an error for unauth user", func() {
		_, err := unallowedUserBMClient.Installer.V2ListFeatureSupportLevels(context.Background(), installer.NewV2ListFeatureSupportLevelsParams())
		Expect(err).Should(HaveOccurred())
	})
})
