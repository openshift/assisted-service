package network

import (
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

// GetHostAddressFamilies tests if a host has addresses in IPv4, in IPv6 family, or both
func GetHostAddressFamilies(host *models.Host) (bool, bool, error) {
	inventory, err := hostutil.UnmarshalInventory(host.Inventory)
	if err != nil {
		return false, false, err
	}
	v4 := false
	v6 := false
	for _, i := range inventory.Interfaces {
		v4 = v4 || len(i.IPV4Addresses) > 0
		v6 = v6 || len(i.IPV6Addresses) > 0
		if v4 && v6 {
			break
		}
	}
	return v4, v6, nil
}

// GetClusterAddressStack tests if all the hosts in a cluster have addresses in IPv4, in IPv6 family, or both (dual stack).
// A dual-stack cluster requires all its hosts to be dual-stack.
func GetClusterAddressStack(hosts []*models.Host) (bool, bool, error) {
	if len(hosts) == 0 {
		return false, false, nil
	}
	v4 := true
	v6 := true
	for _, h := range hosts {
		hostV4, hostV6, err := GetHostAddressFamilies(h)
		if err != nil {
			return false, false, err
		}
		v4 = v4 && hostV4
		v6 = v6 && hostV6
	}
	return v4, v6, nil
}
