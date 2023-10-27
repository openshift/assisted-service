package network

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// Verify if the constrains for dual-stack machine networks are met:
//   - there are exactly two machine networks
//   - the first one is IPv4 subnet
//   - the second one is IPv6 subnet
func VerifyMachineNetworksDualStack(networks []*models.MachineNetwork, isDualStack bool) error {
	if !isDualStack {
		return nil
	}
	if len(networks) != 2 {
		return errors.Errorf("Expected 2 machine networks, found %d", len(networks))
	}
	if !IsIPV4CIDR(string(networks[0].Cidr)) {
		return errors.Errorf("First machine network has to be IPv4 subnet")
	}
	if !IsIPv6CIDR(string(networks[1].Cidr)) {
		return errors.Errorf("Second machine network has to be IPv6 subnet")
	}

	return nil
}

func VerifyServiceNetworksDualStack(networks []*models.ServiceNetwork, isDualStack bool) error {
	if !isDualStack {
		return nil
	}
	if len(networks) != 2 {
		return errors.Errorf("Expected 2 service networks, found %d", len(networks))
	}
	if !IsIPV4CIDR(string(networks[0].Cidr)) {
		return errors.Errorf("First service network has to be IPv4 subnet")
	}
	if !IsIPv6CIDR(string(networks[1].Cidr)) {
		return errors.Errorf("Second service network has to be IPv6 subnet")
	}

	return nil
}

func VerifyClusterNetworksDualStack(networks []*models.ClusterNetwork, isDualStack bool) error {
	if !isDualStack {
		return nil
	}
	if len(networks) != 2 {
		return errors.Errorf("Expected 2 cluster networks, found %d", len(networks))
	}
	if !IsIPV4CIDR(string(networks[0].Cidr)) {
		return errors.Errorf("First cluster network has to be IPv4 subnet")
	}
	if !IsIPv6CIDR(string(networks[1].Cidr)) {
		return errors.Errorf("Second cluster network has to be IPv6 subnet")
	}

	return nil
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
