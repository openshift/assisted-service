package validations

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ImageURLValidator validates image URLs to prevent SSRF attacks.
// It validates schemes, resolves hostnames, and checks IPs against blocked ranges.
type ImageURLValidator struct {
	allowedRegistries    []string
	internalServiceHosts []string
	blockedRanges        []*net.IPNet
	log                  logrus.FieldLogger
	resolver             DNSResolver
}

// DNSResolver is an interface for DNS resolution to allow mocking in tests.
type DNSResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// defaultResolver uses the standard net.Resolver.
type defaultResolver struct {
	resolver *net.Resolver
}

func (r *defaultResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return r.resolver.LookupIP(ctx, network, host)
}

// ImageURLValidatorConfig holds configuration for the ImageURLValidator.
type ImageURLValidatorConfig struct {
	// AllowedRegistries is a comma-separated list of allowed registry domains.
	// If empty, domain validation is skipped (only IP range blocking is enforced).
	AllowedRegistries string `envconfig:"ALLOWED_REGISTRIES" default:""`
	// InternalServiceHosts is a comma-separated list of internal service hostnames
	// that should bypass SSRF IP range validation. These are trusted internal endpoints
	// (e.g., Kubernetes services) that may resolve to private IP addresses.
	//
	// The internal service allowlist permits specific hostnames that would otherwise be blocked:
	//   - "wiremock"               : Test mock service (CI/subsystem tests)
	//   - "postgres"               : Database service
	//   - "assisted-image-service" : Companion service for image handling
	//
	// These are Kubernetes service names that resolve to cluster IPs. Without this
	// allowlist, legitimate inter-service communication would be blocked.
	//
	// Add new internal services here when needed, but be cautious - each entry is a
	// potential SSRF target if that service is compromised.
	// Examples: "wiremock,internal-api,my-service.namespace.svc.cluster.local"
	InternalServiceHosts string `envconfig:"INTERNAL_SERVICE_HOSTS" default:""`
}

// defaultBlockedCIDRs contains CIDR ranges that should be blocked to prevent SSRF attacks.
//
// Blocked IP ranges (SSRF protection):
//   - 10.0.0.0/8       : Class A private network (RFC 1918)
//   - 172.16.0.0/12    : Class B private networks (172.16-31.x.x, RFC 1918)
//   - 192.168.0.0/16   : Class C private network (RFC 1918)
//   - 127.0.0.0/8      : Loopback addresses
//   - 169.254.0.0/16   : Link-local (includes AWS/GCP/Azure metadata endpoints at 169.254.169.254)
//   - ::1, fe80::/10   : IPv6 equivalents
//   - 100.64.0.0/10    : Carrier-grade NAT (RFC 6598)
//
// These ranges are blocked because they can reach internal infrastructure from the
// server's perspective, enabling SSRF attacks to access metadata services, databases,
// internal APIs, and other sensitive resources.
var defaultBlockedCIDRs = []string{
	"127.0.0.0/8",        // Loopback
	"10.0.0.0/8",         // Private Class A
	"172.16.0.0/12",      // Private Class B
	"192.168.0.0/16",     // Private Class C
	"169.254.0.0/16",     // Link-local / AWS metadata
	"0.0.0.0/8",          // Current network
	"100.64.0.0/10",      // Carrier-grade NAT
	"192.0.0.0/24",       // IETF Protocol Assignments
	"192.0.2.0/24",       // TEST-NET-1
	"198.51.100.0/24",    // TEST-NET-2
	"203.0.113.0/24",     // TEST-NET-3
	"224.0.0.0/4",        // Multicast
	"240.0.0.0/4",        // Reserved for future use
	"255.255.255.255/32", // Broadcast
	"::1/128",            // IPv6 loopback
	"fc00::/7",           // IPv6 unique local addresses
	"fe80::/10",          // IPv6 link-local
	"ff00::/8",           // IPv6 multicast
	// Note: IPv4-mapped IPv6 addresses (::ffff:0:0/96) are handled by converting
	// to IPv4 in validateIP() and then checking against IPv4 blocked ranges.
}

// NewImageURLValidator creates a new ImageURLValidator with the specified configuration.
func NewImageURLValidator(config ImageURLValidatorConfig, log logrus.FieldLogger) (*ImageURLValidator, error) {
	return NewImageURLValidatorWithResolver(config, log, nil)
}

// NewImageURLValidatorWithResolver creates a new ImageURLValidator with a custom DNS resolver.
// This is primarily useful for testing.
func NewImageURLValidatorWithResolver(config ImageURLValidatorConfig, log logrus.FieldLogger, resolver DNSResolver) (*ImageURLValidator, error) {
	if log == nil {
		log = logrus.StandardLogger()
	}

	if resolver == nil {
		resolver = &defaultResolver{
			resolver: net.DefaultResolver,
		}
	}

	blockedRanges := make([]*net.IPNet, 0, len(defaultBlockedCIDRs))
	for _, cidr := range defaultBlockedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse blocked CIDR %s", cidr)
		}
		blockedRanges = append(blockedRanges, ipNet)
	}

	var allowedRegistries []string
	if config.AllowedRegistries != "" {
		for _, registry := range strings.Split(config.AllowedRegistries, ",") {
			registry = strings.TrimSpace(registry)
			if registry != "" {
				allowedRegistries = append(allowedRegistries, strings.ToLower(registry))
			}
		}
	}

	var internalServiceHosts []string
	if config.InternalServiceHosts != "" {
		for _, host := range strings.Split(config.InternalServiceHosts, ",") {
			host = strings.TrimSpace(host)
			if host != "" {
				internalServiceHosts = append(internalServiceHosts, strings.ToLower(host))
			}
		}
	}

	return &ImageURLValidator{
		allowedRegistries:    allowedRegistries,
		internalServiceHosts: internalServiceHosts,
		blockedRanges:        blockedRanges,
		log:                  log,
		resolver:             resolver,
	}, nil
}

// ValidateImageURL validates a container image URL for SSRF vulnerabilities.
// It checks the scheme, validates the domain against allowed registries (if configured),
// resolves the hostname to IP addresses, and verifies none of the IPs are in blocked ranges.
func (v *ImageURLValidator) ValidateImageURL(imageURL string) error {
	if imageURL == "" {
		return errors.New("image URL cannot be empty")
	}

	// Container image URLs may or may not have a scheme.
	// If no scheme is present, we treat it as a docker:// URL for validation purposes.
	parsedURL, scheme, err := v.parseImageURL(imageURL)
	if err != nil {
		return err
	}

	// Validate scheme
	if err := v.validateScheme(scheme); err != nil {
		return err
	}

	// Extract hostname (without port)
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return errors.Errorf("could not extract hostname from image URL: %s", imageURL)
	}

	// Check if hostname is a direct IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if err := v.validateIP(ip); err != nil {
			return err
		}
		// When an allowlist is configured, IP-based URLs must also pass the allowlist check.
		// Since IP addresses cannot match domain-based allowlist entries, this will reject
		// IP-based URLs when ALLOWED_REGISTRIES is set, which is the intended security behavior.
		if err := v.validateAllowedRegistry(hostname); err != nil {
			return err
		}
		return nil
	}

	// Validate against allowed registries if configured
	if err := v.validateAllowedRegistry(hostname); err != nil {
		return err
	}

	// Resolve hostname and validate all resolved IPs
	if err := v.validateResolvedIPs(hostname); err != nil {
		return err
	}

	return nil
}

// parseImageURL parses a container image URL, handling both URLs with and without schemes.
// Returns the parsed URL, the detected scheme, and any error.
func (v *ImageURLValidator) parseImageURL(imageURL string) (*url.URL, string, error) {
	// Check for explicit URL schemes
	lowerURL := strings.ToLower(imageURL)

	var scheme string
	var urlToParse string

	if strings.HasPrefix(lowerURL, "docker://") {
		scheme = "docker"
		urlToParse = imageURL
	} else if strings.HasPrefix(lowerURL, "https://") {
		scheme = "https"
		urlToParse = imageURL
	} else if strings.HasPrefix(lowerURL, "http://") {
		scheme = "http"
		urlToParse = imageURL
	} else if strings.HasPrefix(lowerURL, "oci://") {
		scheme = "oci"
		urlToParse = imageURL
	} else if strings.HasPrefix(lowerURL, "file://") {
		// Explicitly reject file:// URLs to prevent local file access
		scheme = "file"
		urlToParse = imageURL
	} else if strings.Contains(lowerURL, "://") {
		// Some other scheme - parse it and let validateScheme handle it
		parsedURL, err := url.Parse(imageURL)
		if err != nil {
			return nil, "", errors.Wrapf(err, "failed to parse image URL: %s", imageURL)
		}
		return parsedURL, parsedURL.Scheme, nil
	} else {
		// No scheme - typical container image reference (e.g., "quay.io/openshift/image:tag")
		// Add a scheme for parsing purposes
		scheme = "docker"
		urlToParse = "docker://" + imageURL
	}

	parsedURL, err := url.Parse(urlToParse)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to parse image URL: %s", imageURL)
	}

	return parsedURL, scheme, nil
}

// validateScheme checks if the URL scheme is allowed for container images.
func (v *ImageURLValidator) validateScheme(scheme string) error {
	allowedSchemes := map[string]bool{
		"docker": true,
		"https":  true,
		"oci":    true,
	}

	if !allowedSchemes[scheme] {
		return errors.Errorf("URL scheme '%s' is not allowed; allowed schemes are: docker, https, oci", scheme)
	}

	return nil
}

// validateAllowedRegistry checks if the hostname is in the allowed registries list.
// If no allowed registries are configured, this check is skipped.
func (v *ImageURLValidator) validateAllowedRegistry(hostname string) error {
	if len(v.allowedRegistries) == 0 {
		// No allowlist configured - skip domain validation
		return nil
	}

	hostname = strings.ToLower(hostname)
	for _, allowed := range v.allowedRegistries {
		if hostname == allowed {
			return nil
		}
		// Allow subdomains of allowed registries
		if strings.HasSuffix(hostname, "."+allowed) {
			return nil
		}
	}

	return errors.Errorf("registry '%s' is not in the allowed registries list: %v", hostname, v.allowedRegistries)
}

// validateResolvedIPs resolves the hostname and validates all resulting IP addresses.
// This function implements a "fail closed" security policy - if DNS resolution fails
// or returns no results, validation fails to prevent potential SSRF attacks through
// DNS manipulation or transient DNS issues.
//
// CRITICAL: Always resolve hostnames to IPs before validation.
// Attackers use DNS rebinding to bypass hostname-based checks:
//  1. First DNS query returns allowed IP (e.g., 8.8.8.8)
//  2. Validation passes
//  3. Second query (during actual request) returns malicious IP (e.g., 169.254.169.254)
//
// By resolving and validating the IP at validation time, we prevent this attack vector.
func (v *ImageURLValidator) validateResolvedIPs(hostname string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ips, err := v.resolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		v.log.WithError(err).Warnf("Failed to resolve hostname %s", hostname)
		// Fail closed: DNS resolution errors could indicate DNS rebinding attacks
		// or attempts to exploit transient DNS issues for SSRF
		return errors.Wrapf(err, "failed to resolve hostname %s", hostname)
	}

	if len(ips) == 0 {
		v.log.Warnf("No IP addresses found for hostname %s", hostname)
		// Fail closed: Empty DNS response could indicate DNS manipulation
		return errors.Errorf("no IP addresses found for hostname %s", hostname)
	}

	for _, ip := range ips {
		if err := v.validateIP(ip); err != nil {
			return errors.Wrapf(err, "hostname %s resolves to blocked IP", hostname)
		}
	}

	return nil
}

// validateIP checks if an IP address is in any of the blocked ranges.
func (v *ImageURLValidator) validateIP(ip net.IP) error {
	// Handle IPv4-mapped IPv6 addresses
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}

	for _, blockedRange := range v.blockedRanges {
		if blockedRange.Contains(ip) {
			return errors.Errorf("IP address %s is in blocked range %s", ip.String(), blockedRange.String())
		}
	}

	return nil
}

// IsIPBlocked is a convenience method to check if a specific IP is blocked.
func (v *ImageURLValidator) IsIPBlocked(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return v.validateIP(ip) != nil
}

// GetAllowedRegistries returns the list of allowed registries.
func (v *ImageURLValidator) GetAllowedRegistries() []string {
	result := make([]string, len(v.allowedRegistries))
	copy(result, v.allowedRegistries)
	return result
}

// GetBlockedRanges returns string representations of blocked CIDR ranges.
func (v *ImageURLValidator) GetBlockedRanges() []string {
	result := make([]string, len(v.blockedRanges))
	for i, r := range v.blockedRanges {
		result[i] = r.String()
	}
	return result
}

// GetInternalServiceHosts returns the list of internal service hosts.
func (v *ImageURLValidator) GetInternalServiceHosts() []string {
	result := make([]string, len(v.internalServiceHosts))
	copy(result, v.internalServiceHosts)
	return result
}

// isInternalServiceHost checks if the hostname matches any configured internal service host.
// Internal service hosts bypass IP range validation since they are trusted internal endpoints.
func (v *ImageURLValidator) isInternalServiceHost(hostname string) bool {
	if len(v.internalServiceHosts) == 0 {
		return false
	}

	hostname = strings.ToLower(hostname)
	for _, internalHost := range v.internalServiceHosts {
		if hostname == internalHost {
			return true
		}
		// Allow subdomains of internal service hosts
		// e.g., "api.internal-service" matches "internal-service"
		if strings.HasSuffix(hostname, "."+internalHost) {
			return true
		}
	}
	return false
}

// ValidateGenericURL validates a generic URL (not specifically a container image URL)
// for SSRF vulnerabilities. This is useful for validating arbitrary URLs that may
// be used for HTTP requests.
// Note: This function does NOT enforce the registry allowlist since it is intended for
// general HTTP endpoints, not container registries. It only validates against blocked
// IP ranges, unless the hostname is configured as an internal service host.
func (v *ImageURLValidator) ValidateGenericURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("URL cannot be empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrapf(err, "failed to parse URL: %s", rawURL)
	}

	// Validate scheme - for generic URLs, only allow http and https
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.Errorf("URL scheme '%s' is not allowed; only http and https are permitted", scheme)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return errors.Errorf("could not extract hostname from URL: %s", rawURL)
	}

	// Check if this is a trusted internal service host
	// Internal service hosts bypass IP range validation since they are
	// admin-configured trusted endpoints that may resolve to private IPs
	if v.isInternalServiceHost(hostname) {
		v.log.Debugf("Skipping IP validation for internal service host: %s", hostname)
		return nil
	}

	// Check if hostname is a direct IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if err := v.validateIP(ip); err != nil {
			return err
		}
		return nil
	}

	// Note: We intentionally do NOT call validateAllowedRegistry here.
	// The registry allowlist is only for container image URLs, not generic HTTP endpoints.
	// Generic URLs are validated only against blocked IP ranges.

	// Resolve hostname and validate all resolved IPs
	if err := v.validateResolvedIPs(hostname); err != nil {
		return err
	}

	return nil
}

// DefaultImageURLValidator is a default validator instance that can be used when
// no custom configuration is needed. It blocks private IP ranges and enforces
// the ALLOWED_REGISTRIES environment variable if set.
var DefaultImageURLValidator *ImageURLValidator

func init() {
	var err error
	// Read configuration from environment variables
	config := ImageURLValidatorConfig{
		AllowedRegistries:    os.Getenv("ALLOWED_REGISTRIES"),
		InternalServiceHosts: os.Getenv("INTERNAL_SERVICE_HOSTS"),
	}
	DefaultImageURLValidator, err = NewImageURLValidator(config, nil)
	if err != nil {
		panic(fmt.Sprintf("failed to create default ImageURLValidator: %v", err))
	}
}
