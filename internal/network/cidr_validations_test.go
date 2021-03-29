package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CIDR validations", func() {
	Context("VerifyClusterCidrSize", func() {
		It("IPv6 overflow", func() {
			Expect(VerifyClusterCidrSize(120, "8::/30", 4)).ToNot(HaveOccurred())
		})
		It("IPv6 no overflow", func() {
			Expect(VerifyClusterCidrSize(120, "8::/80", 4)).ToNot(HaveOccurred())
		})
		It("IPv6 negative", func() {
			Expect(VerifyClusterCidrSize(64, "8::/80", 4)).To(HaveOccurred())
		})
		It("IPv6 zero", func() {
			Expect(VerifyClusterCidrSize(64, "8::/64", 4)).To(HaveOccurred())
		})
		It("IPv6 just enough", func() {
			Expect(VerifyClusterCidrSize(66, "8::/64", 4)).ToNot(HaveOccurred())
		})
	})
	Context("Verify CIDRs", func() {
		It("Machine CIDR 24 OK", func() {
			Expect(VerifyMachineCIDR("1.2.3.0/24")).ToNot(HaveOccurred())
		})
		It("Machine CIDR 26 OK", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/26")).ToNot(HaveOccurred())
		})
		It("Machine CIDR 27 Fail", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/27")).To(HaveOccurred())
		})
		It("Service CIDR 26 Fail", func() {
			Expect(VerifyClusterOrServiceCIDR("1.2.3.0/26")).To(HaveOccurred())
		})
		It("Service CIDR 25 OK", func() {
			Expect(VerifyClusterOrServiceCIDR("1.2.3.0/25")).ToNot(HaveOccurred())
		})
	})
})
