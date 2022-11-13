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
	id       strfmt.UUID
	ip       string
}

var _ = Describe("GetHostnameAndEffectiveRoleByHostID", func() {

	hostRolesIpv4 := []hostNetProfile{{role: models.HostRoleMaster, hostname: "master-0", ip: "1.2.3.1", id: strfmt.UUID(uuid.New().String())},
		{role: models.HostRoleWorker, hostname: "worker-0", ip: "1.2.3.2", id: strfmt.UUID(uuid.New().String())}}
	hostrolesIpv6 := []hostNetProfile{{role: models.HostRoleMaster, hostname: "master-1", ip: "1001:db8::11", id: strfmt.UUID(uuid.New().String())},
		{role: models.HostRoleWorker, hostname: "worker-1", ip: "1001:db8::12", id: strfmt.UUID(uuid.New().String())}}
	clusterID := strfmt.UUID(uuid.New().String())
	infraEnvID := strfmt.UUID(uuid.New().String())
	nonExistantHostID := strfmt.UUID(uuid.New().String())

	Context("resolves hostname and role based on IP", func() {

		testCases := []struct {
			name             string
			hostRolesIpv4    []hostNetProfile
			hostRolesIpv6    []hostNetProfile
			targetID         strfmt.UUID
			expectedRole     models.HostRole
			expectedHostname string
			expectedError    error
		}{
			{
				name:             "resolves correctly when there are only IPv4 interfaces",
				hostRolesIpv4:    hostRolesIpv4,
				targetID:         hostRolesIpv4[0].id,
				expectedRole:     models.HostRoleMaster,
				expectedHostname: "master-0",
			},
			{
				name:             "resolves correctly when there are only IPv6 interfaces",
				hostRolesIpv6:    hostrolesIpv6,
				targetID:         hostrolesIpv6[1].id,
				expectedRole:     models.HostRoleWorker,
				expectedHostname: "worker-1",
			},
			{
				name:             "resolves correctly when there is a mix of IPv4 and IPv6 interfaces",
				hostRolesIpv4:    hostRolesIpv4,
				hostRolesIpv6:    hostrolesIpv6,
				targetID:         hostrolesIpv6[0].id,
				expectedRole:     models.HostRoleMaster,
				expectedHostname: "master-1",
			},
			{
				name:          "unable to resolve when there is a mix of IPv4 and IPv6 interfaces and no match is found",
				hostRolesIpv4: hostRolesIpv4,
				hostRolesIpv6: hostrolesIpv6,
				targetID:      nonExistantHostID,
				expectedError: fmt.Errorf("host with ID %s was not found", nonExistantHostID.String()),
			},
		}

		inventoryCache := make(InventoryCache)
		for i := range testCases {
			test := testCases[i]
			It(test.name, func() {
				hosts := []*models.Host{}
				for _, v := range test.hostRolesIpv4 {
					netAddr := common.NetAddress{Hostname: v.hostname, IPv4Address: []string{fmt.Sprintf("%s/%d", v.ip, 24)}}
					h := hostutil.GenerateTestHostWithNetworkAddress(v.id, infraEnvID, clusterID, v.role, models.HostStatusKnown, netAddr)
					hosts = append(hosts, h)
				}
				for _, v := range test.hostRolesIpv6 {
					netAddr := common.NetAddress{Hostname: v.hostname, IPv6Address: []string{fmt.Sprintf("%s/%d", v.ip, 120)}}
					h := hostutil.GenerateTestHostWithNetworkAddress(v.id, infraEnvID, clusterID, v.role, models.HostStatusKnown, netAddr)
					hosts = append(hosts, h)
				}
				hostname, role, err := GetHostnameAndEffectiveRoleByHostID(test.targetID, hosts, inventoryCache)
				if test.expectedError != nil {
					Expect(err).To(Equal(test.expectedError))
				} else {
					Expect(hostname).To(Equal(test.expectedHostname))
					Expect(role).To(Equal(test.expectedRole))
				}
			})
		}
	})

	Context("Inventory is empty string", func() {
		hostID := strfmt.UUID(uuid.New().String())
		hosts := []*models.Host{{ID: &hostID, Inventory: "", RequestedHostname: "master"}}
		inventoryCache := make(InventoryCache)
		hostname, _, err := GetHostnameAndEffectiveRoleByHostID(hostID, hosts, inventoryCache)
		Expect(hostname).To(Equal(""))
		Expect(err).ToNot(HaveOccurred())
	})
})
