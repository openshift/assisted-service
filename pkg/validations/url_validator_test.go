package validations

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

// publicIPResolver is a mock DNS resolver that always returns public IPs.
type publicIPResolver struct{}

func (r *publicIPResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	// Return a public IP address for any hostname
	return []net.IP{net.ParseIP("8.8.8.8")}, nil
}

var _ = Describe("ImageURLValidator", func() {
	var (
		validator *ImageURLValidator
		log       *logrus.Logger
	)

	BeforeEach(func() {
		log = logrus.New()
		log.SetLevel(logrus.DebugLevel)
	})

	Context("with default configuration (no allowed registries)", func() {
		BeforeEach(func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			// This allows testing URL parsing and scheme validation without real DNS
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("ValidateImageURL", func() {
			It("should accept valid container image URLs", func() {
				validURLs := []string{
					"quay.io/openshift-release-dev/ocp-release:4.14.0-x86_64",
					"registry.redhat.io/openshift/ose-cli:latest",
					"docker://quay.io/openshift/origin-cli:latest",
					"https://quay.io/v2/openshift/origin-cli/manifests/latest",
					"oci://quay.io/openshift/origin-cli:latest",
					"gcr.io/google-containers/pause:3.1",
					"docker.io/library/nginx:latest",
					"my-registry.example.com:5000/image:tag",
				}

				for _, url := range validURLs {
					err := validator.ValidateImageURL(url)
					Expect(err).NotTo(HaveOccurred(), "URL %s should be valid", url)
				}
			})

			It("should reject empty URLs", func() {
				err := validator.ValidateImageURL("")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot be empty"))
			})

			It("should reject http scheme", func() {
				err := validator.ValidateImageURL("http://registry.example.com/image:tag")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("scheme"))
			})

			It("should reject file scheme", func() {
				err := validator.ValidateImageURL("file:///etc/passwd")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("scheme"))
			})

			It("should reject localhost IP addresses", func() {
				blockedURLs := []string{
					"127.0.0.1:5000/image:tag",
					"127.0.0.2:5000/image:tag",
					"docker://127.0.0.1/image",
				}

				for _, url := range blockedURLs {
					err := validator.ValidateImageURL(url)
					Expect(err).To(HaveOccurred(), "URL %s should be blocked", url)
					Expect(err.Error()).To(ContainSubstring("blocked range"))
				}
			})

			It("should reject private network IP addresses", func() {
				blockedURLs := []string{
					"10.0.0.1:5000/image:tag",
					"10.255.255.255/image:tag",
					"172.16.0.1/image:tag",
					"172.31.255.255/image:tag",
					"192.168.1.1/image:tag",
					"192.168.0.100:5000/image:tag",
				}

				for _, url := range blockedURLs {
					err := validator.ValidateImageURL(url)
					Expect(err).To(HaveOccurred(), "URL %s should be blocked", url)
					Expect(err.Error()).To(ContainSubstring("blocked range"))
				}
			})

			It("should reject AWS metadata service IP", func() {
				blockedURLs := []string{
					"169.254.169.254/latest/meta-data/",
					"docker://169.254.169.254/latest/meta-data/iam/security-credentials/",
					"169.254.0.1/something",
				}

				for _, url := range blockedURLs {
					err := validator.ValidateImageURL(url)
					Expect(err).To(HaveOccurred(), "URL %s should be blocked", url)
					Expect(err.Error()).To(ContainSubstring("blocked range"))
				}
			})

			It("should reject IPv6 loopback addresses", func() {
				err := validator.ValidateImageURL("[::1]:5000/image:tag")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("blocked range"))
			})
		})
	})

	Context("with allowed registries configured", func() {
		BeforeEach(func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedRegistries: "quay.io,registry.redhat.io,registry.access.redhat.com",
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept URLs from allowed registries", func() {
			validURLs := []string{
				"quay.io/openshift-release-dev/ocp-release:4.14.0",
				"registry.redhat.io/openshift/ose-cli:latest",
				"registry.access.redhat.com/ubi8/ubi:latest",
				"docker://quay.io/openshift/origin-cli:latest",
			}

			for _, url := range validURLs {
				err := validator.ValidateImageURL(url)
				Expect(err).NotTo(HaveOccurred(), "URL %s should be valid", url)
			}
		})

		It("should accept URLs from subdomains of allowed registries", func() {
			validURLs := []string{
				"cdn.quay.io/image:tag",
				"mirror.registry.redhat.io/image:tag",
			}

			for _, url := range validURLs {
				err := validator.ValidateImageURL(url)
				Expect(err).NotTo(HaveOccurred(), "URL %s should be valid", url)
			}
		})

		It("should reject URLs from non-allowed registries", func() {
			invalidURLs := []string{
				"docker.io/library/nginx:latest",
				"gcr.io/google-containers/pause:3.1",
				"my-private-registry.example.com/image:tag",
			}

			for _, url := range invalidURLs {
				err := validator.ValidateImageURL(url)
				Expect(err).To(HaveOccurred(), "URL %s should be rejected", url)
				Expect(err.Error()).To(ContainSubstring("not in the allowed registries"))
			}
		})

		It("should still reject private IPs even for allowed domains", func() {
			err := validator.ValidateImageURL("127.0.0.1:5000/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked range"))
		})

		It("should reject public IP-based URLs when allowlist is configured", func() {
			// Even though 8.8.8.8 is a public (non-blocked) IP, it should be rejected
			// because it cannot match any domain in the allowlist
			err := validator.ValidateImageURL("8.8.8.8:5000/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not in the allowed registries"))
		})
	})

	Context("DNS resolution validation", func() {
		var mockResolver *mockDNSResolver

		BeforeEach(func() {
			mockResolver = &mockDNSResolver{}
		})

		It("should reject hostnames that resolve to private IPs", func() {
			mockResolver.ips = []net.IP{net.ParseIP("10.0.0.1")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("internal-registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})

		It("should reject hostnames that resolve to AWS metadata IP", func() {
			mockResolver.ips = []net.IP{net.ParseIP("169.254.169.254")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("evil-registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})

		It("should accept hostnames that resolve to public IPs", func() {
			mockResolver.ips = []net.IP{net.ParseIP("8.8.8.8")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("public-registry.example.com/image:tag")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject if any resolved IP is blocked", func() {
			// Mixed IPs - one public, one private
			mockResolver.ips = []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("192.168.1.1")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("mixed-registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})
	})

	Context("ValidateGenericURL", func() {
		BeforeEach(func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept valid https URLs", func() {
			err := validator.ValidateGenericURL("https://api.example.com/endpoint")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept valid http URLs", func() {
			err := validator.ValidateGenericURL("http://api.example.com/endpoint")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject empty URLs", func() {
			err := validator.ValidateGenericURL("")
			Expect(err).To(HaveOccurred())
		})

		It("should reject file scheme", func() {
			err := validator.ValidateGenericURL("file:///etc/passwd")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("scheme"))
		})

		It("should reject private IP addresses", func() {
			blockedURLs := []string{
				"http://127.0.0.1:8080/api",
				"http://10.0.0.1/api",
				"http://192.168.1.1/api",
				"http://169.254.169.254/latest/meta-data/",
			}

			for _, url := range blockedURLs {
				err := validator.ValidateGenericURL(url)
				Expect(err).To(HaveOccurred(), "URL %s should be blocked", url)
			}
		})
	})

	Context("IsIPBlocked", func() {
		BeforeEach(func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should identify blocked IPs", func() {
			blockedIPs := []string{
				"127.0.0.1",
				"10.0.0.1",
				"172.16.0.1",
				"192.168.1.1",
				"169.254.169.254",
				"::1",
			}

			for _, ip := range blockedIPs {
				Expect(validator.IsIPBlocked(ip)).To(BeTrue(), "IP %s should be blocked", ip)
			}
		})

		It("should not block public IPs", func() {
			publicIPs := []string{
				"8.8.8.8",
				"1.1.1.1",
				"208.67.222.222",
				"2001:4860:4860::8888",
			}

			for _, ip := range publicIPs {
				Expect(validator.IsIPBlocked(ip)).To(BeFalse(), "IP %s should not be blocked", ip)
			}
		})

		It("should return false for invalid IP strings", func() {
			Expect(validator.IsIPBlocked("not-an-ip")).To(BeFalse())
		})
	})

	Context("GetAllowedRegistries", func() {
		It("should return empty slice when no registries configured", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())
			Expect(validator.GetAllowedRegistries()).To(BeEmpty())
		})

		It("should return configured registries", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedRegistries: "quay.io, registry.redhat.io",
			}, log)
			Expect(err).NotTo(HaveOccurred())
			registries := validator.GetAllowedRegistries()
			Expect(registries).To(HaveLen(2))
			Expect(registries).To(ContainElements("quay.io", "registry.redhat.io"))
		})
	})

	Context("DefaultImageURLValidator", func() {
		It("should be initialized", func() {
			Expect(DefaultImageURLValidator).NotTo(BeNil())
		})

		It("should block private IPs", func() {
			err := DefaultImageURLValidator.ValidateImageURL("127.0.0.1/image:tag")
			Expect(err).To(HaveOccurred())
		})
	})
})

// mockDNSResolver is a mock DNS resolver for testing.
type mockDNSResolver struct {
	ips []net.IP
	err error
}

func (m *mockDNSResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ips, nil
}

var _ = Describe("ImageURLValidator - Additional Coverage Tests", func() {
	var (
		validator *ImageURLValidator
		log       *logrus.Logger
	)

	BeforeEach(func() {
		log = logrus.New()
		log.SetLevel(logrus.DebugLevel)
	})

	Context("parseImageURL edge cases", func() {
		BeforeEach(func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle URLs with unknown schemes", func() {
			err := validator.ValidateImageURL("ftp://registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("scheme"))
		})

		It("should handle oci:// scheme URLs", func() {
			err := validator.ValidateImageURL("oci://quay.io/openshift/image:tag")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle docker:// scheme URLs with port", func() {
			err := validator.ValidateImageURL("docker://quay.io:443/openshift/image:tag")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject URLs without hostname", func() {
			// This is a malformed URL that parses but has no hostname
			err := validator.ValidateImageURL("docker:///image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not extract hostname"))
		})
	})

	Context("DNS resolution error handling (fail closed)", func() {
		var mockResolver *mockDNSResolver

		BeforeEach(func() {
			mockResolver = &mockDNSResolver{}
		})

		It("should reject URLs when DNS resolution fails (fail closed for security)", func() {
			mockResolver.err = net.UnknownNetworkError("network unreachable")
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to resolve hostname"))
		})

		It("should reject URLs when DNS returns no IPs (fail closed for security)", func() {
			mockResolver.ips = []net.IP{}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("registry.example.com/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no IP addresses found"))
		})
	})

	Context("IP validation edge cases", func() {
		BeforeEach(func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block IPv4-mapped IPv6 addresses for private IPs", func() {
			// ::ffff:10.0.0.1 is the IPv4-mapped IPv6 representation of 10.0.0.1
			Expect(validator.IsIPBlocked("::ffff:10.0.0.1")).To(BeTrue())
		})

		It("should block IPv4-mapped IPv6 addresses for loopback", func() {
			Expect(validator.IsIPBlocked("::ffff:127.0.0.1")).To(BeTrue())
		})

		It("should not block IPv4-mapped IPv6 addresses for public IPs", func() {
			Expect(validator.IsIPBlocked("::ffff:8.8.8.8")).To(BeFalse())
		})

		It("should block carrier-grade NAT range", func() {
			Expect(validator.IsIPBlocked("100.64.0.1")).To(BeTrue())
			Expect(validator.IsIPBlocked("100.127.255.254")).To(BeTrue())
		})

		It("should block TEST-NET ranges", func() {
			Expect(validator.IsIPBlocked("192.0.2.1")).To(BeTrue())    // TEST-NET-1
			Expect(validator.IsIPBlocked("198.51.100.1")).To(BeTrue()) // TEST-NET-2
			Expect(validator.IsIPBlocked("203.0.113.1")).To(BeTrue())  // TEST-NET-3
		})

		It("should block multicast addresses", func() {
			Expect(validator.IsIPBlocked("224.0.0.1")).To(BeTrue())
			Expect(validator.IsIPBlocked("239.255.255.255")).To(BeTrue())
		})

		It("should block reserved addresses", func() {
			Expect(validator.IsIPBlocked("240.0.0.1")).To(BeTrue())
		})

		It("should block broadcast address", func() {
			Expect(validator.IsIPBlocked("255.255.255.255")).To(BeTrue())
		})

		It("should block IPv6 unique local addresses", func() {
			Expect(validator.IsIPBlocked("fc00::1")).To(BeTrue())
			Expect(validator.IsIPBlocked("fd00::1")).To(BeTrue())
		})

		It("should block IPv6 link-local addresses", func() {
			Expect(validator.IsIPBlocked("fe80::1")).To(BeTrue())
		})

		It("should block IPv6 multicast addresses", func() {
			Expect(validator.IsIPBlocked("ff02::1")).To(BeTrue())
		})
	})

	Context("GetBlockedRanges", func() {
		It("should return all blocked CIDR ranges", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())

			ranges := validator.GetBlockedRanges()
			Expect(len(ranges)).To(BeNumerically(">", 0))
			Expect(ranges).To(ContainElement("127.0.0.0/8"))
			Expect(ranges).To(ContainElement("10.0.0.0/8"))
			Expect(ranges).To(ContainElement("169.254.0.0/16"))
		})
	})

	Context("ValidateGenericURL edge cases", func() {
		BeforeEach(func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject docker scheme for generic URLs", func() {
			err := validator.ValidateGenericURL("docker://registry.example.com/image")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("scheme"))
		})

		It("should reject oci scheme for generic URLs", func() {
			err := validator.ValidateGenericURL("oci://registry.example.com/image")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("scheme"))
		})

		It("should reject malformed URLs", func() {
			err := validator.ValidateGenericURL("://invalid-url")
			Expect(err).To(HaveOccurred())
		})

		It("should reject URLs without hostname", func() {
			err := validator.ValidateGenericURL("https:///path/only")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not extract hostname"))
		})

		It("should accept https URLs with ports", func() {
			err := validator.ValidateGenericURL("https://api.example.com:8443/endpoint")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ValidateGenericURL DNS resolution", func() {
		var mockResolver *mockDNSResolver

		BeforeEach(func() {
			mockResolver = &mockDNSResolver{}
		})

		It("should reject hostnames that resolve to private IPs", func() {
			mockResolver.ips = []net.IP{net.ParseIP("192.168.1.1")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateGenericURL("https://internal.example.com/api")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})
	})

	Context("Allowed registries with various formats", func() {
		It("should handle trailing spaces in registry list", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedRegistries: "  quay.io  ,  registry.redhat.io  ",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			registries := validator.GetAllowedRegistries()
			Expect(registries).To(HaveLen(2))
			Expect(registries).To(ContainElement("quay.io"))
			Expect(registries).To(ContainElement("registry.redhat.io"))
		})

		It("should handle empty entries in registry list", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedRegistries: "quay.io,,registry.redhat.io,",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			registries := validator.GetAllowedRegistries()
			Expect(registries).To(HaveLen(2))
		})

		It("should handle case insensitivity for allowed registries", func() {
			var err error
			// Use a mock resolver that returns public IPs for all hostnames
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedRegistries: "QUAY.IO",
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("quay.io/openshift/image:tag")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("NewImageURLValidator edge cases", func() {
		It("should use standard logger when nil is passed", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(validator).NotTo(BeNil())
		})

		It("should use default resolver when nil is passed to WithResolver", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{}, log, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(validator).NotTo(BeNil())
		})
	})

	Context("Internal service hosts", func() {
		var mockResolver *mockDNSResolver

		BeforeEach(func() {
			mockResolver = &mockDNSResolver{}
		})

		It("should bypass IP validation for configured internal service hosts", func() {
			// Configure wiremock as an internal service host
			// Mock DNS returns a private IP (which would normally be blocked)
			mockResolver.ips = []net.IP{net.ParseIP("172.30.102.125")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				InternalServiceHosts: "wiremock,internal-api",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// This should pass because wiremock is in the internal hosts list
			err = validator.ValidateGenericURL("http://wiremock:8070/api/upgrades_info/v1/graph")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should bypass IP validation for subdomains of internal service hosts", func() {
			mockResolver.ips = []net.IP{net.ParseIP("10.0.0.1")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				InternalServiceHosts: "internal-service.namespace.svc.cluster.local",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// Subdomain of internal service host should also bypass validation
			err = validator.ValidateGenericURL("http://api.internal-service.namespace.svc.cluster.local:8080/endpoint")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should still block private IPs for non-internal hosts", func() {
			mockResolver.ips = []net.IP{net.ParseIP("172.30.102.125")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				InternalServiceHosts: "wiremock",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// This should fail because some-other-service is NOT in the internal hosts list
			err = validator.ValidateGenericURL("http://some-other-service:8070/api")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})

		It("should return configured internal service hosts", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				InternalServiceHosts: "wiremock, internal-api, my-service",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			hosts := validator.GetInternalServiceHosts()
			Expect(hosts).To(HaveLen(3))
			Expect(hosts).To(ContainElements("wiremock", "internal-api", "my-service"))
		})

		It("should handle case insensitivity for internal service hosts", func() {
			mockResolver.ips = []net.IP{net.ParseIP("172.16.0.1")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				InternalServiceHosts: "WireMock",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// Should match case-insensitively
			err = validator.ValidateGenericURL("http://wiremock:8070/api")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle empty and whitespace entries in internal hosts list", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				InternalServiceHosts: "  wiremock  ,  ,  internal-api  ",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			hosts := validator.GetInternalServiceHosts()
			Expect(hosts).To(HaveLen(2))
			Expect(hosts).To(ContainElement("wiremock"))
			Expect(hosts).To(ContainElement("internal-api"))
		})

		It("should return empty slice when no internal service hosts configured", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())
			Expect(validator.GetInternalServiceHosts()).To(BeEmpty())
		})

		It("should not affect ValidateImageURL (only ValidateGenericURL)", func() {
			// Internal service hosts only apply to ValidateGenericURL, not container image URLs
			mockResolver.ips = []net.IP{net.ParseIP("172.30.102.125")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				InternalServiceHosts: "wiremock",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// ValidateImageURL should still block private IPs
			err = validator.ValidateImageURL("wiremock:5000/image:tag")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolves to blocked IP"))
		})
	})

	Context("Disconnected environment support (AllowedPrivateCIDRs)", func() {
		var mockResolver *mockDNSResolver

		BeforeEach(func() {
			mockResolver = &mockDNSResolver{}
		})

		It("should allow private IPs when configured for disconnected environments", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8,192.168.0.0/16",
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// These private IPs should now be allowed
			allowedURLs := []string{
				"10.0.0.1:5000/image:tag",
				"10.255.255.255/image:tag",
				"192.168.1.1/image:tag",
				"192.168.100.50:5000/image:tag",
			}

			for _, url := range allowedURLs {
				err := validator.ValidateImageURL(url)
				Expect(err).NotTo(HaveOccurred(), "URL %s should be allowed in disconnected mode", url)
			}
		})

		It("should still block private IPs not in allowed CIDRs", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8", // Only 10.x.x.x is allowed
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// These should still be blocked
			blockedURLs := []string{
				"172.16.0.1/image:tag",
				"192.168.1.1/image:tag",
				"127.0.0.1/image:tag",
			}

			for _, url := range blockedURLs {
				err := validator.ValidateImageURL(url)
				Expect(err).To(HaveOccurred(), "URL %s should still be blocked", url)
			}
		})

		It("should always block AWS metadata endpoint (security boundary)", func() {
			var err error
			// Even with private CIDRs allowed, AWS metadata should be blocked
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8,192.168.0.0/16,172.16.0.0/12",
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// AWS metadata endpoint should always be blocked
			err = validator.ValidateImageURL("169.254.169.254/latest/meta-data/")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked range"))
		})

		It("should allow specific IP with /32 CIDR", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "192.168.1.100/32", // Only this specific IP
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// Specific IP should be allowed
			err = validator.ValidateImageURL("192.168.1.100:5000/image:tag")
			Expect(err).NotTo(HaveOccurred())

			// Other IPs in same subnet should still be blocked
			err = validator.ValidateImageURL("192.168.1.101:5000/image:tag")
			Expect(err).To(HaveOccurred())
		})

		It("should parse single IP addresses as /32 CIDRs", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "192.168.1.100", // Single IP without /32
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// Should work just like /32
			err = validator.ValidateImageURL("192.168.1.100:5000/image:tag")
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateImageURL("192.168.1.101:5000/image:tag")
			Expect(err).To(HaveOccurred())
		})

		It("should return error for invalid CIDR format", func() {
			var err error
			_, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "invalid-cidr",
			}, log)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse allowed private CIDR"))
		})

		It("should allow hostnames that resolve to allowed private IPs", func() {
			mockResolver.ips = []net.IP{net.ParseIP("10.0.0.50")}
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8",
			}, log, mockResolver)
			Expect(err).NotTo(HaveOccurred())

			// Should be allowed because the resolved IP is in allowed private CIDRs
			err = validator.ValidateImageURL("internal-registry.local/image:tag")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return configured allowed private CIDRs", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8, 192.168.1.0/24",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			cidrs := validator.GetAllowedPrivateCIDRs()
			Expect(cidrs).To(HaveLen(2))
			Expect(cidrs).To(ContainElement("10.0.0.0/8"))
			Expect(cidrs).To(ContainElement("192.168.1.0/24"))
		})

		It("should return empty slice when no allowed private CIDRs configured", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{}, log)
			Expect(err).NotTo(HaveOccurred())
			Expect(validator.GetAllowedPrivateCIDRs()).To(BeEmpty())
		})

		It("should work with ValidateGenericURL as well", func() {
			var err error
			validator, err = NewImageURLValidatorWithResolver(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "192.168.0.0/16",
			}, log, &publicIPResolver{})
			Expect(err).NotTo(HaveOccurred())

			// Should be allowed for generic URLs too
			err = validator.ValidateGenericURL("http://192.168.1.100:8080/api")
			Expect(err).NotTo(HaveOccurred())

			// Non-allowed private IP should still be blocked
			err = validator.ValidateGenericURL("http://10.0.0.1:8080/api")
			Expect(err).To(HaveOccurred())
		})

		It("should handle whitespace and empty entries in CIDR list", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "  10.0.0.0/8  ,  ,  192.168.0.0/16  ",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			cidrs := validator.GetAllowedPrivateCIDRs()
			Expect(cidrs).To(HaveLen(2))
		})

		It("should work with IsIPBlocked method", func() {
			var err error
			validator, err = NewImageURLValidator(ImageURLValidatorConfig{
				AllowedPrivateCIDRs: "10.0.0.0/8",
			}, log)
			Expect(err).NotTo(HaveOccurred())

			// 10.x.x.x should not be blocked when in allowed CIDRs
			Expect(validator.IsIPBlocked("10.0.0.1")).To(BeFalse())

			// Other private IPs should still be blocked
			Expect(validator.IsIPBlocked("192.168.1.1")).To(BeTrue())
		})
	})
})
