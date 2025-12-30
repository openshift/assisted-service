package network

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// extractFirstIPAndType extracts the first IP and network type from any network list
func extractFirstIPAndType(networks interface{}) (string, string) {
	switch v := networks.(type) {
	case []*models.MachineNetwork:
		if len(v) > 0 && v[0] != nil {
			return string(v[0].Cidr), "machine_networks"
		}
	case []*models.ServiceNetwork:
		if len(v) > 0 && v[0] != nil {
			return string(v[0].Cidr), "service_networks"
		}
	case []*models.ClusterNetwork:
		if len(v) > 0 && v[0] != nil {
			return string(v[0].Cidr), "cluster_networks"
		}
	case []*models.APIVip:
		if len(v) > 0 && v[0] != nil {
			return string(v[0].IP), "api_vips"
		}
	case []*models.IngressVip:
		if len(v) > 0 && v[0] != nil {
			return string(v[0].IP), "ingress_vips"
		}
	}
	return "", ""
}

// ComputePrimaryIPStack analyzes the provided networks and VIPs to determine
// the primary IP stack based on which IP family appears first in the lists
func ComputePrimaryIPStack(
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
	if ip, networkType := extractFirstIPAndType(machineNetworks); ip != "" {
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = networkType
	}

	// API VIPs
	if ip, networkType := extractFirstIPAndType(apiVips); ip != "" {
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = networkType
	}

	// Ingress VIPs
	if ip, networkType := extractFirstIPAndType(ingressVips); ip != "" {
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = networkType
	}

	// Service Networks
	if ip, networkType := extractFirstIPAndType(serviceNetworks); ip != "" {
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = networkType
	}

	// Cluster Networks
	if ip, networkType := extractFirstIPAndType(clusterNetworks); ip != "" {
		firstIPs = append(firstIPs, ip)
		networkTypeMap[ip] = networkType
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

// ValidateDualStackPartialUpdate validates that updated networks are consistent with existing PrimaryIPStack
func ValidateDualStackPartialUpdate(
	machineNetworks []*models.MachineNetwork,
	apiVips []*models.APIVip,
	ingressVips []*models.IngressVip,
	serviceNetworks []*models.ServiceNetwork,
	clusterNetworks []*models.ClusterNetwork,
	expectedStack common.PrimaryIPStack,
) error {
	// Check each updated network type
	if machineNetworks != nil {
		if err := validateDualStackNetworkConsistency(machineNetworks, expectedStack); err != nil {
			return err
		}
	}
	if apiVips != nil {
		if err := validateDualStackNetworkConsistency(apiVips, expectedStack); err != nil {
			return err
		}
	}
	if ingressVips != nil {
		if err := validateDualStackNetworkConsistency(ingressVips, expectedStack); err != nil {
			return err
		}
	}
	if serviceNetworks != nil {
		if err := validateDualStackNetworkConsistency(serviceNetworks, expectedStack); err != nil {
			return err
		}
	}
	if clusterNetworks != nil {
		if err := validateDualStackNetworkConsistency(clusterNetworks, expectedStack); err != nil {
			return err
		}
	}

	return nil
}

// validateDualStackNetworkConsistency checks if a network list is consistent with the expected primary IP stack
func validateDualStackNetworkConsistency(networks interface{}, expectedStack common.PrimaryIPStack) error {
	// Get the first IP from the network list
	firstIP, actualNetworkType := extractFirstIPAndType(networks)

	if firstIP == "" {
		return nil // No networks to validate
	}

	// Determine the actual IP family of the first IP
	var actualStack common.PrimaryIPStack
	if IsIPV4CIDR(firstIP) || IsIPv4Addr(firstIP) {
		actualStack = common.PrimaryIPStackV4
	} else if IsIPv6CIDR(firstIP) || IsIPv6Addr(firstIP) {
		actualStack = common.PrimaryIPStackV6
	} else {
		return nil // Invalid IP, skip validation
	}

	// Check consistency
	if actualStack != expectedStack {
		return errors.Errorf("Inconsistent IP family order: %s first IP is %s but existing primary IP stack is IPv%d. All networks must have the same IP family first",
			actualNetworkType, firstIP, expectedStack)
	}

	return nil
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
