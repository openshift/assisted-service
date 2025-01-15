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
		It("single-node not enough", func() {
			Expect(VerifyClusterCidrSize(26, "192.168.1.0/25", 1)).To(HaveOccurred())
		})
		It("single-node just enough", func() {
			Expect(VerifyClusterCidrSize(25, "192.168.1.0/25", 1)).ToNot(HaveOccurred())
		})
		It("single-node more than enough", func() {
			Expect(VerifyClusterCidrSize(25, "192.168.1.0/24", 1)).ToNot(HaveOccurred())
		})
	})
	Context("Verify CIDRs", func() {
		It("Machine CIDR 24 OK", func() {
			Expect(VerifyMachineCIDR("1.2.3.0/24", false, false)).ToNot(HaveOccurred())
		})
		It("Machine CIDR 26 OK", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/26", false, false)).ToNot(HaveOccurred())
		})
		It("Machine CIDR 29 Fail", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/29", false, false)).To(HaveOccurred())
		})
		It("Machine CIDR 27 OK", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/27", false, false)).ToNot(HaveOccurred())
		})

		It("Machine CIDR 31 Ok for SNO", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/31", true, false)).ToNot(HaveOccurred())
		})

		It("Machine CIDR 32 Fail for SNO", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/32", true, false)).To(HaveOccurred())
		})

		It("Machine CIDR 30 Ok for user managed load balancer", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/30", false, true)).ToNot(HaveOccurred())
		})

		It("Machine CIDR 31 Fail for user managed load balancer", func() {
			Expect(VerifyMachineCIDR("1.2.3.128/31", false, true)).To(HaveOccurred())
		})

		It("Service CIDR 26 Fail", func() {
			Expect(VerifyClusterOrServiceCIDR("1.2.3.0/26")).To(HaveOccurred())
		})
		It("Service CIDR 25 OK", func() {
			Expect(VerifyClusterOrServiceCIDR("1.2.3.0/25")).ToNot(HaveOccurred())
		})
	})
})
