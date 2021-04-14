package dns

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("DNS tests", func() {
	var (
		baseDNSDomains map[string]string
		dnsApi         DNSApi
	)

	BeforeEach(func() {
		baseDNSDomains = make(map[string]string)
		dnsApi = NewDNSHandler(baseDNSDomains, logrus.New())
	})

	It("get DNS domain success", func() {
		baseDNSDomains["dns.example.com"] = "abc/route53"
		dnsDomain, err := dnsApi.GetDNSDomain("test-cluster", "dns.example.com")
		Expect(err).NotTo(HaveOccurred())
		Expect(dnsDomain.ID).Should(Equal("abc"))
		Expect(dnsDomain.Provider).Should(Equal("route53"))
		Expect(dnsDomain.APIDomainName).Should(Equal("api.test-cluster.dns.example.com"))
		Expect(dnsDomain.APIINTDomainName).Should(Equal("api-int.test-cluster.dns.example.com"))
		Expect(dnsDomain.IngressDomainName).Should(Equal("*.apps.test-cluster.dns.example.com"))
	})
	It("get DNS domain invalid", func() {
		baseDNSDomains["dns.example.com"] = "abc"
		_, err := dnsApi.GetDNSDomain("test-cluster", "dns.example.com")
		Expect(err).To(HaveOccurred())
	})
	It("get DNS domain undefined", func() {
		dnsDomain, err := dnsApi.GetDNSDomain("test-cluster", "dns.example.com")
		Expect(err).NotTo(HaveOccurred())
		Expect(dnsDomain).Should(BeNil())
	})

})

func TestHandler_ListManagedDomains(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNS")
}
