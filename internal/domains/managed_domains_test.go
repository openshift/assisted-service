package domains

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	operations "github.com/openshift/assisted-service/restapi/restapi_v1/operations/managed_domains"
)

func TestHandler_ListManagedDomains(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "managed domains")
}

var _ = Describe("list base domains", func() {
	var (
		h              *Handler
		baseDNSDomains map[string]string
	)
	It("valid", func() {
		baseDNSDomains = map[string]string{
			"example.com": "abc/route53",
		}
		h = NewHandler(baseDNSDomains)
		reply := h.ListManagedDomains(context.Background(), operations.ListManagedDomainsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewListManagedDomainsOK()))
		val, _ := reply.(*operations.ListManagedDomainsOK)
		domains := val.Payload
		Expect(len(domains)).Should(Equal(1))
		Expect(domains[0].Domain).Should(Equal("example.com"))
		Expect(domains[0].Provider).Should(Equal("route53"))
	})
	It("empty", func() {
		baseDNSDomains = map[string]string{}
		h = NewHandler(baseDNSDomains)
		reply := h.ListManagedDomains(context.Background(), operations.ListManagedDomainsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewListManagedDomainsOK()))
		val, _ := reply.(*operations.ListManagedDomainsOK)
		domains := val.Payload
		Expect(len(domains)).Should(Equal(0))
	})
	It("invalid format", func() {
		baseDNSDomains = map[string]string{
			"example.com": "abcroute53",
		}
		h = NewHandler(baseDNSDomains)
		reply := h.ListManagedDomains(context.Background(), operations.ListManagedDomainsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewListManagedDomainsInternalServerError()))
	})
})
