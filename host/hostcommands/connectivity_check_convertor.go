package hostcommands

import (
	"encoding/json"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

func convertHostsToConnectivityCheckParams(currentHostId *strfmt.UUID, hosts []*models.Host, connectivityValidator connectivity.Validator) (string, error) {
	var connectivityCheckHosts models.ConnectivityCheckParams
	for i := range hosts {
		// We don't need to check if host is in some certain states:
		// discovering - Host doesn't have inventory yet
		// disabled - Host does not participate in cluster
		// disconnected - Host does not have connectivity to the network now
		if funk.ContainsString([]string{models.HostStatusDiscovering, models.HostStatusDisconnected},
			swag.StringValue(hosts[i].Status)) {
			continue
		}
		if hosts[i].ID.String() != currentHostId.String() {
			interfaces, err := connectivityValidator.GetHostValidInterfaces(hosts[i])
			if err != nil {
				return "", err
			}
			connectivityCheckHosts = append(connectivityCheckHosts, convertInterfacesToConnectivityCheckHost(hosts[i].ID, interfaces))
		}
	}
	if len(connectivityCheckHosts) == 0 {
		return "", nil
	}
	jsonData, err := json.Marshal(connectivityCheckHosts)
	return string(jsonData), err
}

func convertInterfacesToConnectivityCheckHost(hostId *strfmt.UUID, interfaces []*models.Interface) *models.ConnectivityCheckHost {
	var connectivityHost models.ConnectivityCheckHost
	connectivityHost.HostID = *hostId
	for _, hostInterface := range interfaces {
		var connectivityNic models.ConnectivityCheckNic
		var ipAddresses []string
		connectivityNic.Mac = strfmt.MAC(hostInterface.MacAddress)
		connectivityNic.Name = hostInterface.Name

		for _, ip := range hostInterface.IPV4Addresses {
			ipAddresses = append(ipAddresses, strings.Split(ip, "/")[0])
		}

		for _, ip := range hostInterface.IPV6Addresses {
			ipAddresses = append(ipAddresses, strings.Split(ip, "/")[0])
		}

		connectivityNic.IPAddresses = ipAddresses
		connectivityHost.Nics = append(connectivityHost.Nics, &connectivityNic)
	}
	return &connectivityHost
}
