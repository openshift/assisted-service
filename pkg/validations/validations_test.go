package validations

import (
	"fmt"
	"testing"

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
	tests := []struct {
		domainName string
		valid      bool
	}{
		{
			domainName: "a.com",
			valid:      true,
		},
		{
			domainName: "a",
			valid:      false,
		},
		{
			domainName: "co",
			valid:      false,
		},
		{
			domainName: "aaa",
			valid:      false,
		},
		{
			domainName: "abc.def",
			valid:      true,
		},
		{
			domainName: "-aaa.com",
			valid:      false,
		},
		{
			domainName: "a-aa.com",
			valid:      true,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("Domain name \"%s\"", t.domainName), func() {
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

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
