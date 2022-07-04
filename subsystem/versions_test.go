package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
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
})
