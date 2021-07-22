package host

import (
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

type hostNetProfile struct {
	role     models.HostRole
	hostname string
	ip       string
}

var _ = Describe("GetHostnameAndRoleByIP", func() {

	hostRolesIpv4 := []hostNetProfile{{role: models.HostRoleMaster, hostname: "master-0", ip: "1.2.3.1"}, {role: models.HostRoleWorker, hostname: "worker-0", ip: "1.2.3.2"}}
	hostrolesIpv6 := []hostNetProfile{{role: models.HostRoleMaster, hostname: "master-1", ip: "1001:db8::11"}, {role: models.HostRoleWorker, hostname: "worker-1", ip: "1001:db8::12"}}
	clusterID := strfmt.UUID(uuid.New().String())

	Context("resolves hostname and role based on IP", func() {

		testCases := []struct {
			name             string
			hostRolesIpv4    []hostNetProfile
			hostRolesIpv6    []hostNetProfile
			targetIP         string
			expectedRole     models.HostRole
			expectedHostname string
			expectedError    error
		}{
			{
				name:             "resolves correctly when there are only IPv4 interfaces",
				hostRolesIpv4:    hostRolesIpv4,
				targetIP:         "1.2.3.1",
				expectedRole:     models.HostRoleMaster,
				expectedHostname: "master-0",
			},
			{
				name:             "resolves correctly when there are only IPv6 interfaces",
				hostRolesIpv6:    hostrolesIpv6,
				targetIP:         "1001:db8::12",
				expectedRole:     models.HostRoleWorker,
				expectedHostname: "worker-1",
			},
			{
				name:             "resolves correctly when there is a mix of IPv4 and IPv6 interfaces",
				hostRolesIpv4:    hostRolesIpv4,
				hostRolesIpv6:    hostrolesIpv6,
				targetIP:         "1001:db8::11",
				expectedRole:     models.HostRoleMaster,
				expectedHostname: "master-1",
			},
			{
				name:          "unable to resolve when there is a mix of IPv4 and IPv6 interfaces and no match is found",
				hostRolesIpv4: hostRolesIpv4,
				hostRolesIpv6: hostrolesIpv6,
				targetIP:      "1001:db8::30",
				expectedError: fmt.Errorf("host with IP %s not found in inventory", "1001:db8::30")},
		}

		for i := range testCases {
			test := testCases[i]
			It(test.name, func() {
				hosts := []*models.Host{}
				for _, v := range test.hostRolesIpv4 {
					netAddr := common.NetAddress{Hostname: v.hostname, IPv4Address: []string{fmt.Sprintf("%s/%d", v.ip, 24)}}
					h := hostutil.GenerateTestHostWithNetworkAddress(strfmt.UUID(uuid.New().String()), clusterID, v.role, models.HostStatusKnown, netAddr)
					hosts = append(hosts, h)
				}
				for _, v := range test.hostRolesIpv6 {
					netAddr := common.NetAddress{Hostname: v.hostname, IPv6Address: []string{fmt.Sprintf("%s/%d", v.ip, 120)}}
					h := hostutil.GenerateTestHostWithNetworkAddress(strfmt.UUID(uuid.New().String()), clusterID, v.role, models.HostStatusKnown, netAddr)
					hosts = append(hosts, h)
				}
				hostname, role, err := GetHostnameAndRoleByIP(test.targetIP, hosts)
				if test.expectedError != nil {
					Expect(err).To(Equal(test.expectedError))
				} else {
					Expect(hostname).To(Equal(test.expectedHostname))
					Expect(role).To(Equal(test.expectedRole))
				}

			})
		}
	})

})
