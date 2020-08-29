package validations

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// #nosec
const (
	validSecretFormat   = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	invalidAuthFormat   = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	invalidSecretFormat = "{\"auths\":{\"cloud.openshift.com\":{\"key\":\"abcdef=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"adasfsdf=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"tatastata==\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	validSSHPublicKey   = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain"
	invalidSSHPublicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDI2PBP9RuAHCJ1JvxS0gkK7cm1sMHtdqCYuHzK7fmoMSPeAu+GEPVlBmes825gabO7vUK/pVmcsP9mQLXB0KZ8m/QEBXSO9vmF8dEt5OqtpRLcRzxmcnU1iUs50VSQyEeSxdSV4KA9JuWa+q0f3o3VO+CF6s4kQvQ4lumyCyNSFIBnFCX16+O8syah/UpHUWVqJeHaXCV8qzYKyRvy6nMI5lqCgxe+ENqHkgfkQkgEKHZ8gEnzHtJgewZ3E6fbjQ59eEEvF0zb7WKKWA0YzWOMVGGybj4cFMPQ4Jt7iJ0OZKPBQZMHBcPNrej5lasgcKR7nH5XS0UjHhX5vZJ7e7zONHK4XZj6OjEOXilg3/4rxSn0+QQtT1v0RDXRQhHS6sCyRFV12MqEP8XjPIdBMbE26lRwk3tBwWx7plj3UCVamQid3nY5kslD4X7+cqE8n3bNF922rhCy5STycfEFN3XTs73yKvVPjpro4aQw4BVi4P7B7m7F1d/DqRBuYwWuQ6cLLLLLLLLLLL= root@xxxxxx.xx.xxx.xxx.redhat.com"
)

var _ = Describe("Pull secret validation", func() {

	Context("test secret format", func() {
		It("valid format", func() {
			err := ValidatePullSecret(validSecretFormat)
			Expect(err).Should(BeNil())
		})
		It("invalid format for the auth", func() {
			err := ValidatePullSecret(invalidAuthFormat)
			Expect(err).ShouldNot(BeNil())
		})
		It("invalid format", func() {
			err := ValidatePullSecret(invalidSecretFormat)
			Expect(err).ShouldNot(BeNil())
		})
	})

})

var _ = Describe("SSH Key validation", func() {
	It("valid ssh key", func() {
		err := ValidateSSHPublicKey(validSSHPublicKey)
		Expect(err).Should(BeNil())
	})
	It("invalid ssh key", func() {
		err := ValidateSSHPublicKey(invalidSSHPublicKey)
		Expect(err).ShouldNot(BeNil())
	})
})

type mockRoute53Client struct {
	route53iface.Route53API
}

func (m *mockRoute53Client) ListResourceRecordSets(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
	var output = route53.ListResourceRecordSetsOutput{
		ResourceRecordSets: []*route53.ResourceRecordSet{
			{
				Name: aws.String("api.test.example.com."),
				Type: aws.String("A"),
			},
			{
				Name: aws.String("*.apps.test.example.com."),
				Type: aws.String("A"),
			},
		},
	}
	return &output, nil
}

func (m *mockRoute53Client) GetHostedZone(*route53.GetHostedZoneInput) (*route53.GetHostedZoneOutput, error) {
	var output = route53.GetHostedZoneOutput{
		HostedZone: &route53.HostedZone{
			Name: aws.String("test.example.com"),
		},
	}
	return &output, nil
}

var _ = Describe("DNS Records validation", func() {
	var dnsProvider dnsproviders.Route53

	BeforeEach(func() {
		mockSvc := &mockRoute53Client{}
		dnsProvider = dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: "A",
				TTL:           60,
			},
			HostedZoneID: "abc",
		}
		dnsProvider.SVC = mockSvc
	})

	It("validation success", func() {
		names := []string{"api.test2.example.com", "*.apps.test2.example.com"}
		err := checkDNSRecordsExistence(names, dnsProvider)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation failure - both names already exist", func() {
		names := []string{"api.test.example.com", "*.apps.test.example.com"}
		err := checkDNSRecordsExistence(names, dnsProvider)
		Expect(err).Should(HaveOccurred())
	})
	It("validation failure - one name already exist", func() {
		names := []string{"api.test.example.com", "*.apps.test2.example.com"}
		err := checkDNSRecordsExistence(names, dnsProvider)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Base DNS validation", func() {
	var dnsProvider dnsproviders.Route53

	BeforeEach(func() {
		mockSvc := &mockRoute53Client{}
		dnsProvider = dnsproviders.Route53{
			HostedZoneID: "abc",
		}
		dnsProvider.SVC = mockSvc
	})

	It("validation success", func() {
		err := validateBaseDNS("test.example.com", "abc", dnsProvider)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation success - trailing dots", func() {
		err := validateBaseDNS("test.example.com.", "abc", dnsProvider)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation failure - invalid domain", func() {
		err := validateBaseDNS("test2.example.com", "abc", dnsProvider)
		Expect(err).Should(HaveOccurred())
	})
	It("validation success - valid subdomain", func() {
		err := validateBaseDNS("abc.test.example.com", "abc", dnsProvider)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("validation failure - invalid subdomain", func() {
		err := validateBaseDNS("abc.deftest.example.com", "abc", dnsProvider)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Cluster name validation", func() {
	It("success", func() {
		err := ValidateClusterNameFormat("test-1")
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("invalid format - special character", func() {
		err := ValidateClusterNameFormat("test!")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - capital letter", func() {
		err := ValidateClusterNameFormat("testA")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - starts with number", func() {
		err := ValidateClusterNameFormat("1test")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - ends with hyphen", func() {
		err := ValidateClusterNameFormat("test-")
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Proxy validations", func() {

	Context("test proxy URL", func() {
		It("valid DNS name", func() {
			err := ValidateHTTPProxyFormat("http://proxy.com:3128")
			Expect(err).Should(BeNil())
		})
		It("valid DNS name with user and password", func() {
			err := ValidateHTTPProxyFormat("http://username:pswd@proxy.com")
			Expect(err).Should(BeNil())
		})
		It("valid IP address", func() {
			err := ValidateHTTPProxyFormat("http://10.9.8.7:123")
			Expect(err).Should(BeNil())
		})
		It("valid IP address with user and password", func() {
			err := ValidateHTTPProxyFormat("http://username:pswd@10.9.8.7:123")
			Expect(err).Should(BeNil())
		})
		It("unsupported https schema", func() {
			err := ValidateHTTPProxyFormat("https://proxy.com:3128")
			Expect(err).ShouldNot(BeNil())
			Expect(err.Error()).To(Equal("The URL scheme must be http; https is currently not supported: 'https://proxy.com:3128'"))
		})
		It("invalid empty value", func() {
			err := ValidateHTTPProxyFormat("")
			Expect(err).ShouldNot(BeNil())
			Expect(err.Error()).To(Equal("Proxy URL format is not valid: ''"))
		})
		It("invalid format", func() {
			err := ValidateHTTPProxyFormat("!@#$!@#$")
			Expect(err).ShouldNot(BeNil())
			Expect(err.Error()).To(Equal("Proxy URL format is not valid: '!@#$!@#$'"))
		})
	})

	Context("test no-proxy", func() {
		It("'*' bypass proxy for all destinations", func() {
			err := ValidateNoProxyFormat("*")
			Expect(err).Should(BeNil())
		})
		It("domain name", func() {
			err := ValidateNoProxyFormat("domain.com")
			Expect(err).Should(BeNil())
		})
		It("domain starts with . for all sub-domains", func() {
			err := ValidateNoProxyFormat(".domain.com")
			Expect(err).Should(BeNil())
		})
		It("CIDR", func() {
			err := ValidateNoProxyFormat("10.9.0.0/16")
			Expect(err).Should(BeNil())
		})
		It("IP Address", func() {
			err := ValidateNoProxyFormat("10.9.8.7")
			Expect(err).Should(BeNil())
		})
		It("multiple entries", func() {
			err := ValidateNoProxyFormat("domain.com,10.9.0.0/16,.otherdomain.com,10.9.8.7")
			Expect(err).Should(BeNil())
		})
		It("invalid format", func() {
			err := ValidateNoProxyFormat("...")
			Expect(err).ShouldNot(BeNil())
		})
		It("invalid format of a single value", func() {
			err := ValidateNoProxyFormat("domain.com,...")
			Expect(err).ShouldNot(BeNil())
		})
	})
})

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
