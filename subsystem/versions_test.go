package subsystem

import (
	"context"

	"github.com/filanov/bm-inventory/client/versions"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("test versions", func() {
	It("get versions list", func() {
		reply, err := bmclient.Versions.ListComponentVersions(context.Background(),
			&versions.ListComponentVersionsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(reply.GetPayload().Versions)).To(Equal(6))
	})
})
