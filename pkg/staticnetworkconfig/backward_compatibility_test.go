package staticnetworkconfig_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	snc "github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/sirupsen/logrus"
)

var _ = Describe("ShouldUseNmstateService", func() {
	var staticNetworkGenerator snc.StaticNetworkConfig

	BeforeEach(func() {
		staticNetworkGenerator = snc.New(logrus.New(), snc.Config{MinVersionForNmstateService: common.MinimalVersionForNmstatectl})
	})

	It("returns false when version is empty", func() {
		result, err := staticNetworkGenerator.ShouldUseNmstateService("")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("returns false for version below minimum", func() {
		result, err := staticNetworkGenerator.ShouldUseNmstateService("4.17")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("returns true for the minimum version", func() {
		result, err := staticNetworkGenerator.ShouldUseNmstateService(common.MinimalVersionForNmstatectl)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("returns true for version above minimum", func() {
		result, err := staticNetworkGenerator.ShouldUseNmstateService("4.19")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("returns an error for an invalid version", func() {
		_, err := staticNetworkGenerator.ShouldUseNmstateService("not-a-version")
		Expect(err).To(HaveOccurred())
	})
})
