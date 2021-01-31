package cluster

import (
	"crypto/md5" // #nosec
	"fmt"
	"net/url"
	"strings"

	"github.com/asaskevich/govalidator"
	"github.com/go-openapi/swag"
	"github.com/pkg/errors"
)

// Proxy defines proxy configuration
type Proxy struct {
	HTTPProxy  *string
	HTTPSProxy *string
	NoProxy    *string
}

// NewProxy constructs a proxy instance
func NewProxy(httpProxy, httpsProxy, noProxy *string) *Proxy {
	return &Proxy{
		HTTPProxy:  httpProxy,
		HTTPSProxy: httpsProxy,
		NoProxy:    noProxy,
	}
}

// IsSet returns true if either HTTP, or HTTPS, or
// both were provided
func (p *Proxy) IsSet() bool {
	return (p.HTTPProxy != nil && *p.HTTPProxy != "") ||
		(p.HTTPSProxy != nil && *p.HTTPSProxy != "")
}

// Validate validates the proxy configuration
func (p *Proxy) Validate() error {

	http := swag.StringValue(p.HTTPProxy)
	if http != "" {
		if err := validateHTTPProxyFormat(http); err != nil {
			return errors.Errorf("Failed to validate HTTP Proxy: %s", err)
		}
	}

	https := swag.StringValue(p.HTTPSProxy)
	if https != "" {
		if err := validateHTTPProxyFormat(https); err != nil {
			return errors.Errorf("Failed to validate HTTPS Proxy: %s", err)
		}
	}

	noProxy := swag.StringValue(p.NoProxy)
	if noProxy != "" {
		if err := validateNoProxyFormat(noProxy); err != nil {
			return err
		}
	}

	return nil
}

// Diff detects if two proxy configurations differ.
// Changed proxy settings mean that a new ISO file must be generated
// to include the updated proxy settings
func (p *Proxy) Diff(httpProxy, httpsProxy, noProxy string) bool {
	if httpProxy != swag.StringValue(p.HTTPProxy) ||
		httpsProxy != swag.StringValue(p.HTTPSProxy) ||
		noProxy != swag.StringValue(p.NoProxy) {
		return true
	}
	return false
}

// ComputeHash computes the proxy hash in order to identify changes in proxy settings
func (p *Proxy) ComputeHash() (string, error) {

	var proxyHash string
	if p.HTTPProxy != nil {
		proxyHash += *p.HTTPProxy
	}

	if p.HTTPSProxy != nil {
		proxyHash += *p.HTTPSProxy
	}

	if p.NoProxy != nil {
		proxyHash += *p.NoProxy
	}

	h := md5.New() // #nosec
	_, err := h.Write([]byte(proxyHash))
	if err != nil {
		return "", err
	}

	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs), nil
}

// validateHTTPProxyFormat validates the HTTP Proxy and HTTPS Proxy format
func validateHTTPProxyFormat(proxyURL string) error {

	if !govalidator.IsURL(proxyURL) {
		return errors.Errorf("Proxy URL format is not valid: '%s'", proxyURL)
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		return errors.Errorf("Proxy URL format is not valid: '%s'", proxyURL)
	}

	if u.Scheme == "https" {
		return errors.Errorf("The URL scheme must be http; https is currently not supported: '%s'", proxyURL)
	}

	if u.Scheme != "http" {
		return errors.Errorf("The URL scheme must be http and specified in the URL: '%s'", proxyURL)
	}

	return nil
}

// validateNoProxyFormat validates the no-proxy format which should be a comma-separated list
// of destination domain names, domains, IP addresses or other network CIDRs. A domain can be
// prefaced with '.' to include all subdomains of that domain.
// Use '*' to bypass proxy for all destinations.
func validateNoProxyFormat(noProxy string) error {

	if noProxy == "*" {
		return nil
	}

	domains := strings.Split(noProxy, ",")
	for _, s := range domains {
		s = strings.TrimPrefix(s, ".")
		if govalidator.IsIP(s) {
			continue
		}

		if govalidator.IsCIDR(s) {
			continue
		}

		if govalidator.IsDNSName(s) {
			continue
		}

		return errors.Errorf("NO Proxy format is not valid: '%s'. "+
			"NO Proxy is a comma-separated list of destination domain names, domains, IP addresses or other network CIDRs. "+
			"A domain can be prefaced with '.' to include all subdomains of that domain.", noProxy)
	}

	return nil
}
