package subsystem

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("V2GetClusterDefaultConfig", func() {

	It("InactiveDeletionHours", func() {
		res, err := utils_test.TestContext.UserBMClient.Installer.V2GetClusterDefaultConfig(context.Background(), &installer.V2GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.GetPayload().InactiveDeletionHours).To(Equal(int64(Options.DeregisterInactiveAfter.Hours())))
	})
	It("Default IPv4 networks", func() {
		res, err := utils_test.TestContext.UserBMClient.Installer.V2GetClusterDefaultConfig(context.Background(), &installer.V2GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())

		Expect(res.GetPayload().ClusterNetworksIPV4[0].Cidr).To(Equal(models.Subnet("10.128.0.0/14")))
		Expect(res.GetPayload().ClusterNetworksIPV4[0].HostPrefix).To(Equal(int64(23)))
		Expect(res.GetPayload().ServiceNetworksIPV4[0].Cidr).To(Equal(models.Subnet("172.30.0.0/16")))
	})
	It("Default dual-stack networks", func() {
		res, err := utils_test.TestContext.UserBMClient.Installer.V2GetClusterDefaultConfig(context.Background(), &installer.V2GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())

		Expect(res.GetPayload().ClusterNetworksDualstack[0].Cidr).To(Equal(models.Subnet("10.128.0.0/14")))
		Expect(res.GetPayload().ClusterNetworksDualstack[0].HostPrefix).To(Equal(int64(23)))
		Expect(res.GetPayload().ServiceNetworksDualstack[0].Cidr).To(Equal(models.Subnet("172.30.0.0/16")))

		Expect(res.GetPayload().ClusterNetworksDualstack[1].Cidr).To(Equal(models.Subnet("fd01::/48")))
		Expect(res.GetPayload().ClusterNetworksDualstack[1].HostPrefix).To(Equal(int64(64)))
		Expect(res.GetPayload().ServiceNetworksDualstack[1].Cidr).To(Equal(models.Subnet("fd02::/112")))
	})

	It("Forbidden hostnames", func() {
		res, err := utils_test.TestContext.UserBMClient.Installer.V2GetClusterDefaultConfig(context.Background(), &installer.V2GetClusterDefaultConfigParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.GetPayload().ForbiddenHostnames).To(Equal(`^localhost(?:\d*\.localdomain\d*)?$`))
	})
})
