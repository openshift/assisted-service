package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
)

var _ = Describe("[minimal-set]test versions", func() {
	It("get versions list", func() {
		reply, err := userBMClient.Versions.ListComponentVersions(context.Background(),
			&versions.ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())

		// service, agent, installer, controller
		Expect(len(reply.GetPayload().Versions)).To(Equal(4))
	})

	It("get openshift versions list", func() {
		reply, err := userBMClient.Versions.ListSupportedOpenshiftVersions(context.Background(),
			&versions.ListSupportedOpenshiftVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())

		// 4.6
		Expect(reply.GetPayload()).To(HaveLen(1))
	})
})
