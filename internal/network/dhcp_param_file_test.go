package network

import (
	"net/url"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v2"
)

var _ = Describe("dhcp param file", func() {
	var (
		cluster   *common.Cluster
		clusterId strfmt.UUID
	)
	BeforeEach(func() {
		clusterId = strfmt.UUID(uuid.New().String())
	})
	createTestCluster := func(clusterId strfmt.UUID, dhcpEnabled bool, apiVip, ingressVip string) *common.Cluster {
		return &common.Cluster{
			Cluster: models.Cluster{
				ID:                &clusterId,
				VipDhcpAllocation: swag.Bool(dhcpEnabled),
				APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
				IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
			},
		}
	}
	It("Disabled no vips", func() {
		cluster = createTestCluster(clusterId, false, "", "")
		result, err := GetEncodedDhcpParamFileContents(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeEmpty())
	})
	It("Enabled no vips", func() {
		cluster = createTestCluster(clusterId, true, "", "")
		result, err := GetEncodedDhcpParamFileContents(cluster)
		Expect(err).To(HaveOccurred())
		Expect(result).To(BeEmpty())
	})
	It("Disabled with vips", func() {
		cluster = createTestCluster(clusterId, false, "1.1.1.1", "2.2.2.2")
		result, err := GetEncodedDhcpParamFileContents(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeEmpty())
	})
	It("Enabled with vips", func() {
		cluster = createTestCluster(clusterId, true, "1.1.1.1", "2.2.2.2")
		result, err := GetEncodedDhcpParamFileContents(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeEmpty())
		splits := strings.Split(result, ",")
		Expect(splits).To(HaveLen(2))
		Expect(splits[0]).To(Equal("data:"))
		unescaped, err := url.PathUnescape(splits[1])
		Expect(err).ToNot(HaveOccurred())
		var vipsData vips
		Expect(yaml.Unmarshal([]byte(unescaped), &vipsData)).ToNot(HaveOccurred())
		Expect(vipsData.APIVip.Name).To(Equal("api"))
		Expect(vipsData.APIVip.IpAddress).To(Equal("1.1.1.1"))
		Expect(vipsData.IngressVip.Name).To(Equal("ingress"))
		Expect(vipsData.IngressVip.IpAddress).To(Equal("2.2.2.2"))
	})
})
