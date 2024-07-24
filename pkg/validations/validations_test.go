package validations

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("URL validations", func() {

	Context("test proxy URL", func() {
		var parameters = []struct {
			input, err string
		}{
			{"http://proxy.com:3128", ""},
			{"http://username:pswd@proxy.com", ""},
			{"http://10.9.8.7:123", ""},
			{"http://username:pswd@10.9.8.7:123", ""},
			{"http://[1080:0:0:0:8:800:200C:417A]:8888", ""},
			{
				"http://[1080:0:0:0:8:800:200C:417A]:8888 ",
				"Proxy URL format is not valid: 'http://[1080:0:0:0:8:800:200C:417A]:8888 '",
			},
			{
				"https://proxy.com:3128",
				"The URL scheme must be http; https is currently not supported: 'https://proxy.com:3128'",
			},
			{
				"ftp://proxy.com:3128",
				"The URL scheme must be http and specified in the URL: 'ftp://proxy.com:3128'",
			},
			{
				"httpx://proxy.com:3128",
				"Proxy URL format is not valid: 'httpx://proxy.com:3128'",
			},
			{
				"proxy.com:3128",
				"The URL scheme must be http and specified in the URL: 'proxy.com:3128'",
			},
			{
				"xyz",
				"Proxy URL format is not valid: 'xyz'",
			},
			{
				"http",
				"Proxy URL format is not valid: 'http'",
			},
			{
				"",
				"Proxy URL format is not valid: ''",
			},
			{
				"http://!@#$!@#$",
				"Proxy URL format is not valid: 'http://!@#$!@#$'",
			},
		}

		It("validates proxy URL input", func() {
			for _, param := range parameters {
				err := ValidateHTTPProxyFormat(param.input)
				if param.err == "" {
					Expect(err).Should(BeNil())
				} else {
					Expect(err).ShouldNot(BeNil())
					Expect(err.Error()).To(Equal(param.err))
				}
			}
		})
	})

	Context("test URL", func() {
		var parameters = []struct {
			input, err string
		}{
			{"http://ignition.org:3128", ""},
			{"https://ignition.org:3128", ""},
			{"http://ignition.org:3128/config", ""},
			{"https://ignition.org:3128/config", ""},
			{"http://10.9.8.7:123", ""},
			{"http://10.9.8.7:123/config", ""},
			{"http://[1080::8:800:200c:417a]:123", ""},
			{"http://[1080::8:800:200c:417a]:123/config", ""},
			{
				"http://[1080:0:0:0:8:800:200C:417A]:8888 ",
				"URL 'http://[1080:0:0:0:8:800:200C:417A]:8888 ' format is not valid: parse \"http://[1080:0:0:0:8:800:200C:417A]:8888 \": invalid port \":8888 \" after host",
			},
			{"", "The URL scheme must be http(s) and specified in the URL: ''"},
			{
				"://!@#$!@#$",
				"URL '://!@#$!@#$' format is not valid: parse \"://!@\": missing protocol scheme",
			},
			{
				"ftp://ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'ftp://ignition.com:3128'",
			},
			{
				"httpx://ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'httpx://ignition.com:3128'",
			},
			{
				"ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'ignition.com:3128'",
			},
		}

		It("validates URL input", func() {
			for _, param := range parameters {
				err := ValidateHTTPFormat(param.input)
				if param.err == "" {
					Expect(err).Should(BeNil())
				} else {
					Expect(err).ShouldNot(BeNil())
					Expect(err.Error()).To(Equal(param.err))
				}
			}
		})
	})

	Context("test no-proxy", func() {
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
		It("'*' bypass proxy for all destinations release version", func() {
			err := ValidateNoProxyFormat("*")
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
		It("invalid use of asterisk", func() {
			err := ValidateNoProxyFormat("*,domain.com")
			Expect(err).ShouldNot(BeNil())
		})
	})
})

var _ = Describe("dns name", func() {
	hugeDomainName := "DNSnamescancontainonlyalphabeticalcharactersa-znumericcharacters0-9theminussign-andtheperiod"
	fqnHugeDomain := hugeDomainName + "." + hugeDomainName + "." + hugeDomainName + ".com"
	tests := []struct {
		domainName string
		reason     string
		valid      bool
	}{
		{
			domainName: hugeDomainName,
			reason:     "base domain: should fail - name exceeds max character limit of 63 characters",
			valid:      false,
		},
		{
			domainName: fqnHugeDomain,
			reason:     "full domain: should fail - name exceeds max character limit of 255 characters",
			valid:      false,
		},
		{
			domainName: "a",
			reason:     "base domain: should fail - name requires at least two characters",
			valid:      false,
		},
		{
			domainName: "-",
			reason:     "base domain: should fail - requires alphanumerical characters and not just a dash",
			valid:      false,
		},
		{
			domainName: "a-",
			reason:     "base domain: should fail - names can only end in alphanumerical characters and not a dash",
			valid:      false,
		},
		{
			domainName: "a.c",
			reason:     "full domain: should fail - top level domain must be at least two characters",
			valid:      false,
		},
		{
			domainName: "-aaa.com",
			reason:     "full domain: should fail - labels can only start with alphanumerical characters and not a dash",
			valid:      false,
		},
		{
			domainName: "aaa-.com",
			reason:     "full domain: should fail - labels can only end in an alphanumerical character and not a dash",
			valid:      false,
		},
		{
			domainName: "11.11.11.11",
			reason:     "full domain: should fail - dotted decimal domains (##.##.##.##) are not allowed",
			valid:      false,
		},
		{
			domainName: "1c",
			reason:     "base domain: should pass - two character domain starting with a number",
			valid:      true,
		},
		{
			domainName: "1-c",
			reason:     "base domain: should pass - minimum of two characters and starts and ends with alphanumerical characters",
			valid:      true,
		},
		{
			domainName: "1--c",
			reason:     "base domain: should pass - testing multiple consecutive dashes",
			valid:      true,
		},
		{
			domainName: "exam-ple",
			reason:     "base domain: should pass - multiple characters on either side of dash",
			valid:      true,
		},
		{
			domainName: "exam--ple",
			reason:     "base domain: should pass - multiple characters on either side of multiple consecutive dashes",
			valid:      true,
		},
		{
			domainName: "co",
			reason:     "base domain: should pass - regular two character domain",
			valid:      true,
		},
		{
			domainName: "a.com",
			reason:     "full domain: should pass - just one character as the first subdomain label",
			valid:      true,
		},
		{
			domainName: "abc.def",
			reason:     "full domain: should pass - regular domain",
			valid:      true,
		},
		{
			domainName: "a-aa.com",
			reason:     "full domain: should pass - single dash within label",
			valid:      true,
		},
		{
			domainName: "a--aa.com",
			reason:     "full domain: should pass - multiple consecutive dashes within label",
			valid:      true,
		},
		{
			domainName: "0-example.com0",
			reason:     "full domain: should pass - starting and ending with a number",
			valid:      true,
		},
		{
			domainName: "example-example-example.com",
			reason:     "full domain: should pass - multiple dashes present within a label",
			valid:      true,
		},
		{
			domainName: "example.example-example.com",
			reason:     "full domain: should pass - dash within a different level subdomain label",
			valid:      true,
		},
		{
			domainName: "exam--ple.example--example.com",
			reason:     "full domain: should pass - multiple consecutive dashes within multiple subdomain labels",
			valid:      true,
		},
		{
			domainName: "validateNoWildcardDNS.test.com",
			reason:     "full domain: should pass - allows wildcard dns test",
			valid:      true,
		},
		{
			domainName: "validateNoWildcardDNS.test.com.",
			reason:     "full domain: should pass - allows wildcard dns test (format with period at the end)",
			valid:      true,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("Domain name \"%s\" - testing: \"%s\"", t.domainName, t.reason), func() {
			_, err := ValidateDomainNameFormat(t.domainName)
			if t.valid {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("NTP source", func() {
	tests := []struct {
		ntpSource string
		valid     bool
	}{
		{
			ntpSource: "1.1.1.1",
			valid:     true,
		},
		{
			ntpSource: "clock.redhat.com",
			valid:     true,
		},
		{
			ntpSource: "alias",
			valid:     true,
		},
		{
			ntpSource: "comma,separated,list",
			valid:     true,
		},
		{
			ntpSource: "!jkfd.com",
			valid:     false,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("NTP source \"%s\"", t.ntpSource), func() {
			if t.valid {
				Expect(ValidateAdditionalNTPSource(t.ntpSource)).To(BeTrue())
			} else {
				Expect(ValidateAdditionalNTPSource(t.ntpSource)).To(BeFalse())
			}
		})
	}
})

var _ = Describe("ValidateInstallerArgs", func() {
	It("Parses correctly", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n", "--save-partindex", "1", "--image-url", "https://example.com/image"}
		err := ValidateInstallerArgs(args)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Denies unexpected arguments", func() {
		args := []string{"--not-supported", "value"}
		err := ValidateInstallerArgs(args)
		Expect(err).To(HaveOccurred())
	})

	It("Succeeds with an empty list", func() {
		err := ValidateInstallerArgs([]string{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Denies unexpected values with pipe", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8|echo"}
		err := ValidateInstallerArgs(args)
		Expect(err).To(HaveOccurred())
	})

	It("Denies unexpected values with command and value", func() {
		args := []string{"--append-karg", "echo add"}
		err := ValidateInstallerArgs(args)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ValidateHostname", func() {
	tests := []struct {
		hostname    string
		description string
		valid       bool
	}{
		{
			hostname:    "ocp-master-1",
			description: "Succeeds with a valid hostname",
			valid:       true,
		},
		{
			hostname:    "1-ocp-master-1",
			description: "Succeeds with a valid hostname starting with number",
			valid:       true,
		},
		{
			hostname:    "ocp-master.1",
			description: "Succeeds with a valid hostname containing dash",
			valid:       true,
		},
		{
			hostname:    "OCP-Master-1",
			description: "Fails with an invalid hostname containing capital letter",
			valid:       false,
		},
		{
			hostname:    "-ocp-master-1",
			description: "Fails with an invalid hostname starts with dash",
			valid:       false,
		},
		{
			hostname:    ".ocp-master-1",
			description: "Fails with an invalid hostname starts with a period",
			valid:       false,
		},
		{
			hostname:    "ocp-master-1-ocp-master-1-ocp-master-1-ocp-master-1-ocp-master-1",
			description: "Fails with an invalid hostname that's too long (> 63 characters)",
			valid:       false,
		},
	}
	for _, t := range tests {
		t := t
		It(t.description, func() {
			err := ValidateHostname(t.hostname)
			if t.valid {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("ValidateTags", func() {
	tests := []struct {
		tags  string
		valid bool
	}{
		{
			tags:  "tag1,tag2,tag3",
			valid: true,
		},
		{
			tags:  "tag 1,tag 2,tag 3",
			valid: true,
		},
		{
			tags:  "tag1,tag 2",
			valid: true,
		},
		{
			tags:  "tag1",
			valid: true,
		},
		{
			tags:  "",
			valid: true,
		},
		{
			tags:  "tag1 , tag2",
			valid: false,
		},
		{
			tags:  " tag1 , tag2 ",
			valid: false,
		},
		{
			tags:  "tag1 ,tag2",
			valid: false,
		},
		{
			tags:  "tag1, tag2",
			valid: false,
		},
		{
			tags:  ",",
			valid: false,
		},
		{
			tags:  ",,",
			valid: false,
		},
		{
			tags:  "tag,,",
			valid: false,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("Cluster Tags: \"%s\"", t.tags), func() {
			err := ValidateTags(t.tags)
			if t.valid {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"Tags should be a comma-separated list (e.g. tag1,tag2,tag3). " +
						"Each tag can consist of the following characters: Alphanumeric (aA-zZ, 0-9), underscore (_) and white-spaces."))
			}
		})
	}
})

var _ = Describe("ValidateCaCertificate", func() {
	It("Valid certificate", func() {
		// Encode and validate certificate
		cert, err := GenerateTestCertificate()
		Expect(err).ToNot(HaveOccurred())
		encodedCert := base64.StdEncoding.EncodeToString([]byte(cert))
		err = ValidateCaCertificate(encodedCert)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Invalid certificate", func() {
		encodedCert := base64.StdEncoding.EncodeToString([]byte("invalid"))
		err := ValidateCaCertificate(encodedCert)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unable to parse certificate"))
	})

	It("Invalid certificate encoding", func() {
		err := ValidateCaCertificate("invalid")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to decode certificate"))
	})
})

func GenerateTestCertificate() (string, error) {
	// Generate a test certificate
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			Organization:  []string{"Company, INC."},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Golden Gate Bridge"},
			PostalCode:    []string{"94016"},
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", err
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &certPrivKey.PublicKey, certPrivKey)
	if err != nil {
		return "", err
	}
	certPEM := new(bytes.Buffer)
	if err := pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}); err != nil {
		return "", err
	}

	// Encode certificate
	return certPEM.String(), nil
}

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
