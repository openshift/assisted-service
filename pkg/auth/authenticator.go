package auth

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type AuthType string

const (
	TypeEmpty      AuthType = ""
	TypeNone       AuthType = "none"
	TypeRHSSO      AuthType = "rhsso"
	TypeLocal      AuthType = "local"
	TypeAgentLocal AuthType = "agent-installer-local"
)

// Authenticator defines the interface for authentication handlers.
// The interface maintains backward compatibility with go-swagger generated code.
// Implementations should use CreateAuthenticator to create context-aware authenticators
// that extract client IP from HTTP requests for security audit logging.
type Authenticator interface {
	CreateAuthenticator() func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator
	AuthUserAuth(token string) (interface{}, error)
	AuthAgentAuth(token string) (interface{}, error)
	AuthURLAuth(token string) (interface{}, error)
	AuthImageAuth(token string) (interface{}, error)
	AuthWatcherAuth(token string) (interface{}, error)
	AuthType() AuthType
	EnableOrgTenancy() bool
	EnableOrgBasedFeatureGates() bool
}

type Config struct {
	AuthType       AuthType `envconfig:"AUTH_TYPE" default:""`
	JwkCert        string   `envconfig:"JWKS_CERT"`
	JwkCertURL     string   `envconfig:"JWKS_URL" default:"https://api.openshift.com/.well-known/jwks.json"`
	ECPublicKeyPEM string   `envconfig:"EC_PUBLIC_KEY_PEM"`
	// Will be split with "," as separator
	AllowedDomains             string   `envconfig:"ALLOWED_DOMAINS" default:""`
	AdminUsers                 []string `envconfig:"ADMIN_USERS" default:""`
	EnableOrgTenancy           bool     `envconfig:"ENABLE_ORG_TENANCY" default:"false"`
	EnableOrgBasedFeatureGates bool     `envconfig:"ENABLE_ORG_BASED_FEATURE_GATES" default:"false"`

	// Account lockout configuration for brute force protection
	EnableAccountLockout      bool `envconfig:"ENABLE_ACCOUNT_LOCKOUT" default:"true"`
	LockoutMaxAttempts        int  `envconfig:"LOCKOUT_MAX_ATTEMPTS" default:"5"`
	LockoutDurationMinutes    int  `envconfig:"LOCKOUT_DURATION_MINUTES" default:"15"`
	LockoutWindowMinutes      int  `envconfig:"LOCKOUT_WINDOW_MINUTES" default:"5"`
	LockoutExponentialBackoff bool `envconfig:"LOCKOUT_EXPONENTIAL_BACKOFF" default:"true"`

	// TrustedProxyCIDRs is a comma-separated list of CIDR ranges for trusted proxies.
	// X-Forwarded-For and X-Real-IP headers are only trusted when requests come from these ranges.
	// If empty, forwarded headers are ignored and RemoteAddr is always used.
	// Example: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/8"
	TrustedProxyCIDRs string `envconfig:"TRUSTED_PROXY_CIDRS" default:""`
}

// TrustedProxyChecker validates whether a remote address is from a trusted proxy.
//
// Trusted proxy validation is critical for accurate client identification. Without it,
// attackers can spoof X-Forwarded-For headers to:
//   - Bypass per-IP rate limits and lockouts
//   - Attribute their attacks to innocent IPs
//   - Evade IP-based blocking and audit trails
//
// Only trust forwarded headers when the direct connection comes from a known proxy IP.
type TrustedProxyChecker struct {
	trustedNetworks []*net.IPNet
}

// NewTrustedProxyChecker creates a TrustedProxyChecker from a comma-separated list of CIDR ranges.
func NewTrustedProxyChecker(cidrs string) *TrustedProxyChecker {
	checker := &TrustedProxyChecker{}
	if cidrs == "" {
		return checker
	}

	for _, cidr := range strings.Split(cidrs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as a single IP
			ip := net.ParseIP(cidr)
			if ip != nil {
				var mask net.IPMask
				if ip.To4() != nil {
					mask = net.CIDRMask(32, 32)
				} else {
					mask = net.CIDRMask(128, 128)
				}
				network = &net.IPNet{IP: ip, Mask: mask}
			} else {
				continue // Skip invalid entries
			}
		}
		checker.trustedNetworks = append(checker.trustedNetworks, network)
	}
	return checker
}

// IsTrusted checks if the given IP address is from a trusted proxy.
func (c *TrustedProxyChecker) IsTrusted(ipStr string) bool {
	if len(c.trustedNetworks) == 0 {
		return false
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, network := range c.trustedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// HasTrustedProxies returns true if any trusted proxy networks are configured.
func (c *TrustedProxyChecker) HasTrustedProxies() bool {
	return len(c.trustedNetworks) > 0
}

func NewAuthenticator(cfg *Config, ocmClient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) (a Authenticator, err error) {
	switch cfg.AuthType {
	case TypeRHSSO:
		a = NewRHSSOAuthenticator(cfg, ocmClient, log, db)
	case TypeNone:
		a = NewNoneAuthenticator(log)
	case TypeLocal:
		a, err = NewLocalAuthenticator(cfg, log, db)
	case TypeAgentLocal:
		a, err = NewAgentLocalAuthenticator(cfg, log)
	default:
		err = fmt.Errorf("invalid authenticator type %v", cfg.AuthType)
	}

	log.Infof("Created %s authenticator", cfg.AuthType)
	return
}

// GetLockoutPolicy returns a LockoutPolicy from the Config settings.
// Zero or negative values are replaced with defaults to prevent accidental misconfiguration.
func (c *Config) GetLockoutPolicy() LockoutPolicy {
	policy := LockoutPolicy{
		Enabled:         c.EnableAccountLockout,
		MaxAttempts:     c.LockoutMaxAttempts,
		LockoutDuration: time.Duration(c.LockoutDurationMinutes) * time.Minute,
		WindowDuration:  time.Duration(c.LockoutWindowMinutes) * time.Minute,
		UseExponential:  c.LockoutExponentialBackoff,
	}
	// Validate and apply defaults for invalid values
	defaults := DefaultLockoutPolicy()
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = defaults.MaxAttempts
	}
	if policy.LockoutDuration <= 0 {
		policy.LockoutDuration = defaults.LockoutDuration
	}
	if policy.WindowDuration <= 0 {
		policy.WindowDuration = defaults.WindowDuration
	}
	return policy
}
