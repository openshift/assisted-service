package host

import (
	"encoding/json"
	"strings"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
)

func convertHostsToConnectivityCheckParams(currentHostId *strfmt.UUID, hosts []*models.Host, connectivityValidator connectivity.Validator) (string, error) {
	var connectivityCheckHosts models.ConnectivityCheckParams
	for i := range hosts {
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
		connectivityNic.Mac = hostInterface.MacAddress
		connectivityNic.Name = hostInterface.Name
		for _, ip := range hostInterface.IPV4Addresses {
			ipAddresses = append(ipAddresses, strings.Split(ip, "/")[0])
		}
		connectivityNic.IPAddresses = ipAddresses
		connectivityHost.Nics = append(connectivityHost.Nics, &connectivityNic)
	}
	return &connectivityHost
}
