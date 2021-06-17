package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
)

var _ = Describe("GetClusterDefaultConfig", func() {

	It("InactiveDeletionHours", func() {
		res, err := userBMClient.Installer.GetClusterDefaultConfig(context.Background(), &installer.GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.GetPayload().InactiveDeletionHours).To(Equal(int64(Options.DeregisterInactiveAfter.Hours())))
	})
})
