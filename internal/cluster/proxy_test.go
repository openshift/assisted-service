package cluster

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Proxy validations", func() {

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
				err := validateHTTPProxyFormat(param.input)
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
			err := validateNoProxyFormat("domain.com")
			Expect(err).Should(BeNil())
		})
		It("domain starts with . for all sub-domains", func() {
			err := validateNoProxyFormat(".domain.com")
			Expect(err).Should(BeNil())
		})
		It("CIDR", func() {
			err := validateNoProxyFormat("10.9.0.0/16")
			Expect(err).Should(BeNil())
		})
		It("IP Address", func() {
			err := validateNoProxyFormat("10.9.8.7")
			Expect(err).Should(BeNil())
		})
		It("multiple entries", func() {
			err := validateNoProxyFormat("domain.com,10.9.0.0/16,.otherdomain.com,10.9.8.7")
			Expect(err).Should(BeNil())
		})
		It("'*' bypass proxy for all destinations", func() {
			err := validateNoProxyFormat("*")
			Expect(err).Should(BeNil())
		})
		It("invalid format", func() {
			err := validateNoProxyFormat("...")
			Expect(err).ShouldNot(BeNil())
		})
		It("invalid format of a single value", func() {
			err := validateNoProxyFormat("domain.com,...")
			Expect(err).ShouldNot(BeNil())
		})
	})
})
