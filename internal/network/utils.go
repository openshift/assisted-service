package network

import (
	"net"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// GetHostAddressFamilies tests if a host has addresses in IPv4, in IPv6 family, or both
func GetHostAddressFamilies(host *models.Host) (bool, bool, error) {
	inventory, err := common.UnmarshalInventory(host.Inventory)
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

// Get configured address families from cluster configuration based on the CIDRs (machine-network-cidr, cluster-network-cidr,
// service-network-cidr)
func GetConfiguredAddressFamilies(cluster *common.Cluster) (ipv4 bool, ipv6 bool, err error) {

	for _, cidr := range common.GetNetworksCidrs(cluster) {
		if cidr == nil || *cidr == "" {
			continue
		}
		_, _, err = net.ParseCIDR(*cidr)
		if err != nil {
			return false, false, errors.Wrapf(err, "%s is not a valid cidr", *cidr)
		}
		if IsIPV4CIDR(*cidr) {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	return
}

func GetAddressFamilies(networks interface{}) (ipv4 bool, ipv6 bool, err error) {
	addresses := make([]models.Subnet, 0)
	ipv4 = false
	ipv6 = false

	switch v := networks.(type) {
	case []*models.ClusterNetwork:
		for _, net := range v {
			addresses = append(addresses, net.Cidr)
		}
	case []*models.ServiceNetwork:
		for _, net := range v {
			addresses = append(addresses, net.Cidr)
		}
	case []*models.MachineNetwork:
		for _, net := range v {
			addresses = append(addresses, net.Cidr)
		}
	default:
		return false, false, errors.New("Provided invalid network list")
	}

	for _, cidr := range addresses {
		if IsIPV4CIDR(string(cidr)) {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}

	return ipv4, ipv6, nil
}

func IsMachineCidrAvailable(cluster *common.Cluster) bool {
	return common.IsSliceNonEmpty(cluster.MachineNetworks)
}

func GetMachineCidrById(cluster *common.Cluster, index int) string {
	return string(cluster.MachineNetworks[index].Cidr)
}

func DerefMachineNetworks(obj interface{}) []*models.MachineNetwork {
	switch v := obj.(type) {
	case []*models.MachineNetwork:
		return v
	default:
		return nil
	}
}

func DerefServiceNetworks(obj interface{}) []*models.ServiceNetwork {
	switch v := obj.(type) {
	case []*models.ServiceNetwork:
		return v
	default:
		return nil
	}
}

func DerefClusterNetworks(obj interface{}) []*models.ClusterNetwork {
	switch v := obj.(type) {
	case []*models.ClusterNetwork:
		return v
	default:
		return nil
	}
}
