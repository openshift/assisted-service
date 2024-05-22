package models

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

const (
	baseDomainRegex     = `^[a-z\d]([\-]*[a-z\d]+)+$`
	dnsNameRegex        = `^([a-z\d]([\-]*[a-z\d]+)*\.)+[a-z\d]+([\-]*[a-z\d]+)+$`
	wildCardDomainRegex = `^(validateNoWildcardDNS\.).+\.?$`
)

func ValidateDomainNameFormat(dnsDomainName string) (int32, error) {
	domainName := dnsDomainName
	wildCardMatched, wildCardMatchErr := regexp.MatchString(wildCardDomainRegex, dnsDomainName)
	if wildCardMatchErr == nil && wildCardMatched {
		trimmedDomain := strings.TrimPrefix(dnsDomainName, "validateNoWildcardDNS.")
		domainName = strings.TrimSuffix(trimmedDomain, ".")
	}
	matched, err := regexp.MatchString(baseDomainRegex, domainName)
	if err != nil {
		return http.StatusInternalServerError, errors.Wrapf(err, "Single DNS base domain validation for %s", dnsDomainName)
	}
	if matched && len(domainName) > 1 && len(domainName) < 63 {
		return 0, nil
	}
	matched, err = regexp.MatchString(dnsNameRegex, domainName)
	if err != nil {
		return http.StatusInternalServerError, errors.Wrapf(err, "DNS name validation for %s", dnsDomainName)
	}

	if !matched || isDottedDecimalDomain(domainName) || len(domainName) > 255 {
		return http.StatusBadRequest, errors.Errorf(
			"DNS format mismatch: %s domain name is not valid. Must match regex [%s], be no more than 255 characters, and not be in dotted decimal format (##.##.##.##)",
			dnsDomainName, dnsNameRegex)
	}
	return 0, nil
}

// RFC 1123 (https://datatracker.ietf.org/doc/html/rfc1123#page-13)
// states that domains cannot resemble the format ##.##.##.##
func isDottedDecimalDomain(domain string) bool {
	regex := `([\d]+\.){3}[\d]+`
	return regexp.MustCompile(regex).MatchString(domain)
}
