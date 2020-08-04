package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/versions"
)

var _ = Describe("test versions", func() {
	It("get versions list", func() {
		reply, err := userBMClient.Versions.ListComponentVersions(context.Background(),
			&versions.ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(reply.GetPayload().Versions)).To(Equal(6))
	})
})
