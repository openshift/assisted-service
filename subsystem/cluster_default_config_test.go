package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
)

var _ = Describe("V2GetClusterDefaultConfig", func() {

	It("InactiveDeletionHours", func() {
		res, err := userBMClient.Installer.V2GetClusterDefaultConfig(context.Background(), &installer.V2GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.GetPayload().InactiveDeletionHours).To(Equal(int64(Options.DeregisterInactiveAfter.Hours())))
	})
})
