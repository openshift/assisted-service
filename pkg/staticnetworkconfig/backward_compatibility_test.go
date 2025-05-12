package staticnetworkconfig_test

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	snc "github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/sirupsen/logrus"
)

var _ = Describe("ShouldUseNmstateService", func() {
	var (
		staticNetworkGenerator = snc.New(logrus.New(), snc.Config{MinVersionForNmstateService: common.MinimalVersionForNmstatectl})
		hostsYAML              = `[{ "network_yaml": "%s" }]`
		withMacIdentifier      = `interfaces:
- name: eth0
  type: ethernet
  state: up
  identifier: mac-address
  mac-address: 02:00:00:80:12:14
  ipv4:
    enabled: true
    address:
      - ip: 192.0.2.1
        prefix-length: 24`
		withoutMacIdentifier = `interfaces:
- name: eth0
  type: ethernet
  state: up
  ipv4:
    enabled: true
    address:
      - ip: 192.0.2.1
        prefix-length: 24`
		withAutoDnsSetToFalse = `interfaces:
- ipv4:
    auto-dns: false
    dhcp: true
    enabled: true
  ipv6:
    enabled: false
  name: eth0
  state: up
  type: ethernet
`
	)
	table.DescribeTable("different scenarios", func(YAML, version string, expectedResult bool) {
		escapedYamlContent, err := escapeYAMLForJSON(YAML)
		Expect(err).NotTo(HaveOccurred())

		shouldUseNmstateService, err := staticNetworkGenerator.ShouldUseNmstateService(fmt.Sprintf(hostsYAML, escapedYamlContent), version)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldUseNmstateService).To(Equal(expectedResult))
	},
		table.Entry("If the YAML contains a mac-identifier, and the version is >= MinimalVersionForNmstatectl,  we shouldn't use the nmstate service flow", withMacIdentifier, common.MinimalVersionForNmstatectl, false),
		table.Entry("If the YAML contains a mac-identifier, and the version is < MinimalVersionForNmstatectl,  we shouldn't use the nmstate service flow", withMacIdentifier, "4.12", false),
		table.Entry("If the YAML doesn't contain a mac-identifier and the version is >= MinimalVersionForNmstatectl, we should use the nmstate service flow", withoutMacIdentifier, common.MinimalVersionForNmstatectl, true),
		table.Entry("If the YAML doesn't contain a mac-identifier, and the version < MinimalVersionForNmstatectl we shouldn't use the nmstate service flow.", withoutMacIdentifier, "4.12", false),
		table.Entry("If the YAML contains a auto-dns: false, and the version >= MinimalVersionForNmstatectl we shouldn't use the nmstate service flow.", withAutoDnsSetToFalse, common.MinimalVersionForNmstatectl, false))
})
