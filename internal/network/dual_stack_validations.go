package network

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// GetPrimaryIPStack analyzes the provided networks and VIPs to determine
// the primary IP stack based on which IP family appears first in the lists
func GetPrimaryIPStack(
	machineNetworks []*models.MachineNetwork,
	apiVips []*models.APIVip,
	ingressVips []*models.IngressVip,
	serviceNetworks []*models.ServiceNetwork,
	clusterNetworks []*models.ClusterNetwork,
) (*common.PrimaryIPStack, error) {

	// Collect first IPs in order and track their network types
	var firstIPs []string
	networkTypeMap := make(map[string]string)

	// Machine Networks
	if len(machineNetworks) > 0 && machineNetworks[0] != nil {
		ip := string(machineNetworks[0].Cidr)
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = "machine_networks"
	}

	// API VIPs
	if len(apiVips) > 0 && apiVips[0] != nil {
		ip := string(apiVips[0].IP)
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = "api_vips"
	}

	// Ingress VIPs
	if len(ingressVips) > 0 && ingressVips[0] != nil {
		ip := string(ingressVips[0].IP)
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = "ingress_vips"
	}

	// Service Networks
	if len(serviceNetworks) > 0 && serviceNetworks[0] != nil {
		ip := string(serviceNetworks[0].Cidr)
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = "service_networks"
	}

	// Cluster Networks
	if len(clusterNetworks) > 0 && clusterNetworks[0] != nil {
		ip := string(clusterNetworks[0].Cidr)
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = "cluster_networks"
	}

	if len(firstIPs) == 0 {
		return nil, nil // No networks provided, no primary stack determination
	}

	// Check consistency - all first IPs should be the same family
	var primaryStack *common.PrimaryIPStack
	firstIPSeen := false
	var firstIP string

	for _, ip := range firstIPs {
		var currentStack common.PrimaryIPStack

		if IsIPV4CIDR(ip) || IsIPv4Addr(ip) {
			currentStack = common.PrimaryIPStackV4
		} else if IsIPv6CIDR(ip) || IsIPv6Addr(ip) {
			currentStack = common.PrimaryIPStackV6
		} else {
			continue // Skip invalid IPs
		}

		if !firstIPSeen {
			// First valid IP - set the primary stack
			primaryStack = &currentStack
			firstIP = ip
			firstIPSeen = true
		} else {
			// Subsequent valid IPs - check consistency
			if *primaryStack != currentStack {
				return nil, errors.Errorf("Inconsistent IP family order: %s first IP is %s but %s first IP is %s. All networks must have the same IP family first",
					networkTypeMap[firstIP], firstIP,
					networkTypeMap[ip], ip)
			}
		}
	}

	return primaryStack, nil
}

// supportsIPv6PrimaryDualStack checks if the OpenShift version supports IPv6-primary dual-stack
// IPv6-primary dual-stack is supported starting from OCP 4.12
func supportsIPv6PrimaryDualStack(openshiftVersion string) bool {
	if openshiftVersion == "" {
		return false
	}

	isOlderVersion, err := common.BaseVersionLessThan(common.MinimalVersionForIPV6PrimaryWithDualStack, openshiftVersion)
	if err != nil {
		// If we can't parse the version, be conservative and don't allow IPv6-primary
		return false
	}

	return !isOlderVersion
}

// ValidateDualStackOrder performs version-aware dual-stack validation on any IP-related items
func ValidateDualStackOrder(
	items []string,
	itemType string,
	itemUnit string, // "subnet" or "address"
	openshiftVersion string,
	isIPv4Func func(string) bool,
	isIPv6Func func(string) bool,
) error {
	if len(items) != 2 {
		return errors.Errorf("Expected 2 %s, found %d", itemType, len(items))
	}

	allowIPv6Primary := supportsIPv6PrimaryDualStack(openshiftVersion)

	if allowIPv6Primary {
		// For OCP 4.12+: Allow any order, just ensure we have one IPv4 and one IPv6
		hasIPv4 := false
		hasIPv6 := false

		for _, item := range items {
			if isIPv4Func(item) {
				hasIPv4 = true
			} else if isIPv6Func(item) {
				hasIPv6 = true
			}
		}

		if !hasIPv4 {
			return errors.Errorf("dual-stack %s must include exactly one IPv4 %s", itemType, itemUnit)
		}
		if !hasIPv6 {
			return errors.Errorf("dual-stack %s must include exactly one IPv6 %s", itemType, itemUnit)
		}
	} else {
		// For OCP < 4.12: Maintain original IPv4-first requirement
		if !isIPv4Func(items[0]) {
			return errors.Errorf("First %s has to be IPv4 %s (IPv6-primary dual-stack requires OpenShift 4.12+), got %s", itemType, itemUnit, items[0])
		}
		if !isIPv6Func(items[1]) {
			return errors.Errorf("Second %s has to be IPv6 %s, got %s", itemType, itemUnit, items[1])
		}
	}
	return nil
}

// VerifyMachineNetworksDualStack Verify if the constraints for dual-stack machine networks are met:
//   - there are exactly two machine networks
//   - for OCP < 4.12: the first one is IPv4 subnet and the second one is IPv6 subnet
//   - for OCP >= 4.12: one is IPv4 subnet and one is IPv6 subnet (in any order)
func VerifyMachineNetworksDualStack(networks []*models.MachineNetwork, isDualStack bool, openshiftVersion string) error {
	if !isDualStack {
		return nil
	}

	cidrs := make([]string, len(networks))
	for i, network := range networks {
		cidrs[i] = string(network.Cidr)
	}

	return ValidateDualStackOrder(cidrs, "machine networks", "subnet", openshiftVersion, IsIPV4CIDR, IsIPv6CIDR)
}

// VerifyServiceNetworksDualStack Verify if the constraints for dual-stack service networks are met:
//   - there are exactly two service networks
//   - for OCP < 4.12: the first one is IPv4 subnet and the second one is IPv6 subnet
//   - for OCP >= 4.12: one is IPv4 subnet and one is IPv6 subnet (in any order)
func VerifyServiceNetworksDualStack(networks []*models.ServiceNetwork, isDualStack bool, openshiftVersion string) error {
	if !isDualStack {
		return nil
	}
	cidrs := make([]string, len(networks))
	for i, network := range networks {
		cidrs[i] = string(network.Cidr)
	}
	return ValidateDualStackOrder(cidrs, "service networks", "subnet", openshiftVersion, IsIPV4CIDR, IsIPv6CIDR)
}

// VerifyClusterNetworksDualStack Verify if the constraints for dual-stack cluster networks are met:
//   - there are exactly two cluster networks
//   - for OCP < 4.12: the first one is IPv4 subnet and the second one is IPv6 subnet
//   - for OCP >= 4.12: one is IPv4 subnet and one is IPv6 subnet (in any order)
func VerifyClusterNetworksDualStack(networks []*models.ClusterNetwork, isDualStack bool, openshiftVersion string) error {
	if !isDualStack {
		return nil
	}
	cidrs := make([]string, len(networks))
	for i, network := range networks {
		cidrs[i] = string(network.Cidr)
	}
	return ValidateDualStackOrder(cidrs, "cluster networks", "subnet", openshiftVersion, IsIPV4CIDR, IsIPv6CIDR)
}

// Verify if the current Cluster configuration indicates that it is a dual-stack cluster. This
// happens based on the following rule - if at least one of Machine, Service or Cluster Networks
// is a list containing both IPv4 and IPv6 address, we mark the cluster as dual-stack.
func CheckIfClusterIsDualStack(c *common.Cluster) bool {
	if c == nil {
		return false
	}
	return CheckIfNetworksAreDualStack(c.MachineNetworks, c.ServiceNetworks, c.ClusterNetworks)
}

func CheckIfNetworksAreDualStack(machineNetworks []*models.MachineNetwork, serviceNetworks []*models.ServiceNetwork, clusterNetworks []*models.ClusterNetwork) bool {
	var err error
	var ipv4, ipv6 bool
	dualStack := false

	ipv4, ipv6, err = GetAddressFamilies(machineNetworks)
	if err != nil {
		return false
	}
	dualStack = ipv4 && ipv6

	if !dualStack {
		ipv4, ipv6, err = GetAddressFamilies(serviceNetworks)
		if err != nil {
			return false
		}
		dualStack = ipv4 && ipv6
	}

	if !dualStack {
		ipv4, ipv6, err = GetAddressFamilies(clusterNetworks)
		if err != nil {
			return false
		}
		dualStack = ipv4 && ipv6
	}

	return dualStack
}

// Wrapper around CheckIfClusterIsDualStack function allowing to pass models.Cluster instead of
// common.Cluster object.
func CheckIfClusterModelIsDualStack(c *models.Cluster) bool {
	cluster := common.Cluster{}
	if c != nil {
		cluster.MachineNetworks = c.MachineNetworks
		cluster.ServiceNetworks = c.ServiceNetworks
		cluster.ClusterNetworks = c.ClusterNetworks
	}
	return CheckIfClusterIsDualStack(&cluster)
}
