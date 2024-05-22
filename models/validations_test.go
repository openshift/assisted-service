package models

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNS validations tests")
}

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
