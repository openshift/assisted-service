package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("GetClusterAppsDomain", func() {
	It("derives apps.<name>.<base> when ingress_domain is empty", func() {
		c := &models.Cluster{Name: "abc-int", BaseDNSDomain: "example.com"}
		Expect(GetClusterAppsDomain(c)).To(Equal("apps.abc-int.example.com"))
	})

	It("uses ingress_domain when set", func() {
		c := &models.Cluster{
			Name:           "abc-int",
			BaseDNSDomain:  "example.com",
			IngressDomain:  "abc.example.com",
		}
		Expect(GetClusterAppsDomain(c)).To(Equal("abc.example.com"))
	})

	It("strips a leading wildcard prefix from ingress_domain", func() {
		c := &models.Cluster{IngressDomain: "*.abc.example.com"}
		Expect(GetClusterAppsDomain(c)).To(Equal("abc.example.com"))
	})

	It("returns empty when cluster metadata is incomplete", func() {
		Expect(GetClusterAppsDomain(nil)).To(Equal(""))
		Expect(GetClusterAppsDomain(&models.Cluster{Name: "x"})).To(Equal(""))
		Expect(GetClusterAppsDomain(&models.Cluster{BaseDNSDomain: "y"})).To(Equal(""))
	})
})

var _ = Describe("GetAppsDomainProbeHost", func() {
	It("builds console probe under derived apps domain", func() {
		c := &models.Cluster{Name: "abc-int", BaseDNSDomain: "example.com"}
		Expect(GetAppsDomainProbeHost(c)).To(Equal(
			constants.AppsSubDomainNameHostDNSValidation + ".apps.abc-int.example.com"))
	})

	It("builds console probe under custom ingress domain", func() {
		c := &models.Cluster{IngressDomain: "abc.example.com"}
		Expect(GetAppsDomainProbeHost(c)).To(Equal(
			constants.AppsSubDomainNameHostDNSValidation + ".abc.example.com"))
	})
})
