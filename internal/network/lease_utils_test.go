package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

const apiLease = `lease {
  interface "api";
  fixed-address 10.0.0.16;
  option subnet-mask 255.255.255.0;
  option dhcp-lease-time 3600;
  option routers 10.0.0.138;
  option dhcp-message-type 5;
  option dhcp-server-identifier 10.0.0.138;
  option domain-name-servers 10.0.0.138;
  option domain-name "Home";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}`

const ingressLease = `lease {
  interface "ingress";
  fixed-address 10.0.0.17;
  option subnet-mask 255.255.255.0;
  option dhcp-lease-time 3600;
  option routers 10.0.0.138;
  option dhcp-message-type 5;
  option dhcp-server-identifier 10.0.0.138;
  option domain-name-servers 10.0.0.138;
  option domain-name "Home";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}`

const twoLeases = `lease {
  interface "macvlan";
  fixed-address 10.0.0.17;
  option subnet-mask 255.255.255.0;
  option dhcp-lease-time 3600;
  option routers 10.0.0.138;
  option dhcp-message-type 5;
  option dhcp-server-identifier 10.0.0.138;
  option domain-name-servers 10.0.0.138;
  option domain-name "Home";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}
lease {
  interface "macvlan";
  fixed-address 10.0.0.16;
  option subnet-mask 255.255.255.0;
  option dhcp-lease-time 3600;
  option routers 10.0.0.138;
  option dhcp-message-type 5;
  option dhcp-server-identifier 10.0.0.138;
  option domain-name-servers 10.0.0.138;
  option domain-name "Home";
  renew 0 2020/10/25 14:48:38;
  rebind 0 2020/10/25 15:11:32;
  expire 0 2020/10/25 15:19:02;
}`

var _ = Describe("dhcp param file", func() {
	It("Format_lease", func() {
		r := FormatLease(apiLease)
		Expect(r).To(ContainSubstring("renew never;"))
		Expect(r).To(ContainSubstring("rebind never;"))
		Expect(r).To(ContainSubstring("expire never;"))
	})
	Context("VerifyLease", func() {
		It("valid lease", func() {
			Expect(VerifyLease(apiLease)).ToNot(HaveOccurred())
		})
		It("2 leases", func() {
			Expect(VerifyLease(twoLeases)).To(HaveOccurred())
		})
		It("Invalid lease", func() {
			Expect(VerifyLease(apiLease[1:])).To(HaveOccurred())
			Expect(VerifyLease("l" + apiLease)).To(HaveOccurred())
		})
	})
	It("Encoded", func() {
		cluster := &common.Cluster{
			ApiVipLease:     apiLease,
			IngressVipLease: ingressLease,
		}
		Expect(GetEncodedApiVipLease(cluster)).To(Equal("data:,lease%20%7B%0A%20%20interface%20%22api%22%3B%0A%20%20fixed-address%2010.0.0.16%3B%0A%20%20option%20subnet-mask%20255.255.255.0%3B%0A%20%20option%20dhcp-lease-time%203600%3B%0A%20%20option%20routers%2010.0.0.138%3B%0A%20%20option%20dhcp-message-type%205%3B%0A%20%20option%20dhcp-server-identifier%2010.0.0.138%3B%0A%20%20option%20domain-name-servers%2010.0.0.138%3B%0A%20%20option%20domain-name%20%22Home%22%3B%0A%20%20renew%200%202020%2F10%2F25%2014:48:38%3B%0A%20%20rebind%200%202020%2F10%2F25%2015:11:32%3B%0A%20%20expire%200%202020%2F10%2F25%2015:19:02%3B%0A%7D"))
		Expect(GetEncodedIngressVipLease(cluster)).To(Equal("data:,lease%20%7B%0A%20%20interface%20%22ingress%22%3B%0A%20%20fixed-address%2010.0.0.17%3B%0A%20%20option%20subnet-mask%20255.255.255.0%3B%0A%20%20option%20dhcp-lease-time%203600%3B%0A%20%20option%20routers%2010.0.0.138%3B%0A%20%20option%20dhcp-message-type%205%3B%0A%20%20option%20dhcp-server-identifier%2010.0.0.138%3B%0A%20%20option%20domain-name-servers%2010.0.0.138%3B%0A%20%20option%20domain-name%20%22Home%22%3B%0A%20%20renew%200%202020%2F10%2F25%2014:48:38%3B%0A%20%20rebind%200%202020%2F10%2F25%2015:11:32%3B%0A%20%20expire%200%202020%2F10%2F25%2015:19:02%3B%0A%7D"))
	})
})
