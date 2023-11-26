package dns

import (
	"context"
	"fmt"
	"testing"

	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
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

	Context("DNS domain", func() {

		generateDomainName := func(labelLen, labelCount int) string {
			buff := make([]byte, labelLen*labelCount+labelCount-1)
			dot := labelLen
			for i := range buff {
				if i == dot {
					buff[i] = '.'
					dot = dot + labelLen + 1
				} else {
					buff[i] = 'a'
				}
			}
			return string(buff)
		}

		tests := []struct {
			baseDomain  string
			clusterName string
			valid       bool
		}{
			{
				baseDomain:  "example.com",
				clusterName: generateDomainName(64, 1),
				valid:       false,
			},
			{
				baseDomain:  generateDomainName(64, 2),
				clusterName: "cluster-name",
				valid:       false,
			},
			{
				baseDomain:  generateDomainName(60, 3),
				clusterName: generateDomainName(6, 1),
				valid:       false,
			},
			{
				baseDomain:  generateDomainName(62, 2),
				clusterName: generateDomainName(60, 1),
				valid:       true,
			},
			{
				baseDomain:  "example.com",
				clusterName: "cluster-name",
				valid:       true,
			},
		}
		for _, t := range tests {
			t := t
			It(fmt.Sprintf("Base domain: %s, cluster name: %s", t.baseDomain, t.clusterName), func() {
				err := dnsApi.ValidateDNSName(t.clusterName, t.baseDomain)
				if t.valid {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(HaveOccurred())
				}
			})
		}
	})
})

var _ = Describe("DNS record set update tests", func() {

	var (
		ctx            context.Context
		ctrl           *gomock.Controller
		mockProviders  *MockDNSProviderFactory
		baseDNSDomains map[string]string
		dns            DNSApi
		cluster        *common.Cluster
	)

	BeforeEach(func() {
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				Name:            "ut-cluster",
				BaseDNSDomain:   "dns-test.com",
				APIVips:         []*models.APIVip{{IP: "10.56.20.50"}},
				IngressVips:     []*models.IngressVip{{IP: "2001:db8:3c4d:15::2b"}},
				MachineNetworks: []*models.MachineNetwork{{Cidr: "10.56.20.0/24"}},
			},
		}
		ctx = context.Background()
		baseDNSDomains = make(map[string]string)
		baseDNSDomains[cluster.BaseDNSDomain] = "unittest/some-provider"
		ctrl = gomock.NewController(GinkgoT())
		mockProviders = NewMockDNSProviderFactory(ctrl)
		dns = NewDNSHandlerWithProviders(baseDNSDomains, logrus.New(), mockProviders)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("create with no supported domain", func() {
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), gomock.Any()).Times(0)
		err := dns.CreateDNSRecordSets(ctx, &common.Cluster{})
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("delete with no supported domain", func() {
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), gomock.Any()).Times(0)
		err := dns.DeleteDNSRecordSets(ctx, &common.Cluster{})
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("create DNS record set multi-node", func() {
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().CreateRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain), network.GetApiVipById(cluster, 0)).Times(1)
		mockProv.EXPECT().CreateRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain), network.GetIngressVipById(cluster, 0)).Times(1)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockProv)
		err := dns.CreateDNSRecordSets(ctx, cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("delete DNS record set multi-node", func() {
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().DeleteRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain), network.GetApiVipById(cluster, 0)).Times(1)
		mockProv.EXPECT().DeleteRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain), network.GetIngressVipById(cluster, 0)).Times(1)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockProv)
		err := dns.DeleteDNSRecordSets(ctx, cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation successful when records do not exist multi-node", func() {
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(2).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(2).Return("", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation fails when first name already has an A record multi-node", func() {
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("non-empty", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).Should(HaveOccurred())
	})
	It("validation fails when second name already has an A record multi-node", func() {
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("non-empty", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).Should(HaveOccurred())
	})
	It("validation fails when first name already has an AAAA record multi-node", func() {
		mockATypeProv := NewMockDNSProvider(ctrl)
		mockATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockATypeProv)
		mockAAAATypeProv := NewMockDNSProvider(ctrl)
		mockAAAATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("non-empty", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockAAAATypeProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).Should(HaveOccurred())
	})
	It("validation fails when second name already has an AAAA record multi-node", func() {
		mockATypeProv := NewMockDNSProvider(ctrl)
		mockATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockATypeProv)
		mockAAAATypeProv := NewMockDNSProvider(ctrl)
		mockAAAATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockAAAATypeProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("non-empty", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockAAAATypeProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).Should(HaveOccurred())
	})
	It("create DNS record set SNO", func() {
		bootstrapIP := "10.56.20.62"
		setUpSNO(cluster, bootstrapIP)
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().CreateRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProv.EXPECT().CreateRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProv.EXPECT().CreateRecordSet(fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(3).Return(mockProv)
		err := dns.CreateDNSRecordSets(ctx, cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("delete DNS record set SNO", func() {
		bootstrapIP := "10.56.20.65"
		setUpSNO(cluster, bootstrapIP)
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().DeleteRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProv.EXPECT().DeleteRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProv.EXPECT().DeleteRecordSet(fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain), bootstrapIP).Times(1)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(3).Return(mockProv)
		err := dns.DeleteDNSRecordSets(ctx, cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation successful when records do not exist SNO", func() {
		bootstrapIP := "10.56.20.67"
		setUpSNO(cluster, bootstrapIP)
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(2).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(2).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(2).Return("", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "AAAA").Times(1).Return(mockProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation fails when a record already exists for SNO", func() {
		bootstrapIP := "10.56.20.59"
		setUpSNO(cluster, bootstrapIP)
		mockProv := NewMockDNSProvider(ctrl)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("*.apps.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("", nil)
		mockProv.EXPECT().GetRecordSet(fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain)).Times(1).Return("non-empty", nil)
		mockProviders.EXPECT().GetProviderByRecordType(gomock.Any(), "A").Times(1).Return(mockProv)
		domain, err := dns.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
		Expect(err).ShouldNot(HaveOccurred())
		err = dns.ValidateDNSRecords(*cluster, domain)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Record type by IP address", func() {
	It("AAAA type for IPv4 address", func() {
		Expect(getDNSRecordType("192.168.1.12")).To(Equal("A"))
	})
	It("A type for IPv6 address", func() {
		Expect(getDNSRecordType("2001:db8:3c4d:15::2b")).To(Equal("AAAA"))
	})
})

var _ = Describe("Default DNS provider tests", func() {

	var (
		domain    *DNSDomain
		providers DNSProviderFactory
	)

	BeforeEach(func() {
		domain = &DNSDomain{
			Provider: "route53",
		}
		providers = &defaultDNSProviderFactory{logrus.New()}
	})

	It("default provider is used when no provider factory specified", func() {
		dns := NewDNSHandler(make(map[string]string), logrus.New())
		h, ok := dns.(*handler)
		Expect(ok).To(BeTrue())
		Expect(h.providerFactory).To(BeAssignableToTypeOf(providers))
	})
	It("return nil when unknown provider", func() {
		p := providers.GetProviderByRecordType(&DNSDomain{}, "AAAA")
		Expect(p).To(BeNil())
	})
	It("provider for AAAA records", func() {
		p := providers.GetProviderByRecordType(domain, "AAAA")
		r53, ok := p.(dnsproviders.Route53)
		Expect(ok).To(BeTrue())
		Expect(r53.RecordSet.RecordSetType).To(Equal("AAAA"))
	})
	It("provider for A records", func() {
		p := providers.GetProviderByRecordType(domain, "A")
		r53, ok := p.(dnsproviders.Route53)
		Expect(ok).To(BeTrue())
		Expect(r53.RecordSet.RecordSetType).To(Equal("A"))
	})
})

var _ = Describe("Base DNS domain validation", func() {

	var (
		ctrl *gomock.Controller
		dns  DNSApi
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProvider := NewMockDNSProvider(ctrl)
		mockProvider.EXPECT().GetDomainName().Times(1).Return("test.example.com", nil)
		mockProviderFact := NewMockDNSProviderFactory(ctrl)
		mockProviderFact.EXPECT().GetProvider(gomock.Any()).Times(1).Return(mockProvider)
		dns = NewDNSHandlerWithProviders(make(map[string]string), logrus.New(), mockProviderFact)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("validation success", func() {
		domain := &DNSDomain{Name: "test.example.com"}
		err := dns.ValidateBaseDNS(domain)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation success - trailing dots", func() {
		domain := &DNSDomain{Name: "test.example.com."}
		err := dns.ValidateBaseDNS(domain)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation failure - invalid domain", func() {
		domain := &DNSDomain{Name: "test2.example.com"}
		err := dns.ValidateBaseDNS(domain)
		Expect(err).Should(HaveOccurred())
	})
	It("validation success - valid subdomain", func() {
		domain := &DNSDomain{Name: "abc.test.example.com"}
		err := dns.ValidateBaseDNS(domain)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation failure - invalid subdomain", func() {
		domain := &DNSDomain{Name: "abc.deftest.example.com"}
		err := dns.ValidateBaseDNS(domain)
		Expect(err).Should(HaveOccurred())
	})
})

func TestHandler_ListManagedDomains(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNS")
}

func setUpSNO(cluster *common.Cluster, bootstrapIPAddress string) {
	cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
	cluster.Hosts = make([]*models.Host, 1)
	cluster.Hosts[0] = &models.Host{
		Bootstrap: true,
		Inventory: fmt.Sprintf("{\"interfaces\":[{\"ipv4_addresses\":[\"%s/24\"]}]}", bootstrapIPAddress),
	}
}
