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

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
