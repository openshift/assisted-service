package network

import (
	"net"

	"github.com/pkg/errors"
)

// Minimum mask size to allow 128 addresses
const MinMaskDelta = 7

// VerifyCIDRsNotOverlap returns true if one of the CIDRs is a subset of the other.
func verifyCIDRsNotOverlap(acidr, bcidr *net.IPNet) error {
	if acidr.Contains(bcidr.IP) || bcidr.Contains(acidr.IP) {
		return errors.Errorf("CIDRS %s and %s overlap", acidr.String(), bcidr.String())
	}
	return nil
}

func VerifyCIDRsNotOverlap(aCidrStr, bCidrStr string) error {
	if aCidrStr == "" || bCidrStr == "" {
		return nil
	}
	_, acidr, err := net.ParseCIDR(aCidrStr)
	if err != nil {
		return err
	}
	_, bcidr, err := net.ParseCIDR(bCidrStr)
	if err != nil {
		return err
	}
	return verifyCIDRsNotOverlap(acidr, bcidr)
}

// SubnetCIDR checks if the given IP net is a valid CIDR.
func VerifySubnetCIDR(cidrStr string) error {
	ip, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return err
	}
	ones, bits := cidr.Mask.Size()
	// We would like to allow at least 128 addresses.  Therefore, ones must be not greater than (bits-7)
	if ones < 1 || ones > bits-MinMaskDelta {
		return errors.Errorf("Address mask size must be between 1 to %d and must include at least 128 addresses", bits-7)
	}
	if cidr.IP.IsUnspecified() {
		return errors.Errorf("The specified CIDR %s is invalid because its resulting routing prefix matches the unspecified address", cidrStr)
	}
	if !ip.Equal(cidr.IP) {
		return errors.Errorf("%s is not a valid network CIDR", (&net.IPNet{IP: ip, Mask: cidr.Mask}).String())
	}
	return nil
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func VerifyClusterCidrSize(hostNetworkPrefix int, clusterNetworkCIDR string, numberOfHosts int) error {
	_, cidr, err := net.ParseCIDR(clusterNetworkCIDR)
	if err != nil {
		return err
	}
	clusterNetworkPrefix, bits := cidr.Mask.Size()
	// We would like to allow at least 128 addresses.  Therefore, hostNetworkPrefix must be not greater than (bits-7)
	if hostNetworkPrefix > bits-MinMaskDelta {
		return errors.Errorf("Host prefix, now %d, must be less than or equal to %d to allow at least 128 addresses", hostNetworkPrefix, bits-MinMaskDelta)
	}
	requestedNumHosts := max(4, numberOfHosts)
	// 63 to avoid overflow
	possibleNumHosts := uint64(1) << min(63, max(hostNetworkPrefix-clusterNetworkPrefix, 0))
	if uint64(requestedNumHosts) > possibleNumHosts {
		return errors.Errorf("Cluster network CIDR prefix %d does not contain enough addresses for %d hosts each one with %d prefix (%d addresses)",
			clusterNetworkPrefix, requestedNumHosts, hostNetworkPrefix, uint64(1)<<min(63, bits-hostNetworkPrefix))
	}
	return nil
}

func VerifyClusterCIDRsNotOverlap(machineNetworkCidr, clusterNetworkCidr, serviceNetworkCidr string, userManagedNetworking bool) error {
	if !userManagedNetworking {
		err := VerifyCIDRsNotOverlap(machineNetworkCidr, serviceNetworkCidr)
		if err != nil {
			return errors.Wrap(err, "MachineNetworkCIDR and ServiceNetworkCIDR")
		}
		err = VerifyCIDRsNotOverlap(machineNetworkCidr, clusterNetworkCidr)
		if err != nil {
			return errors.Wrap(err, "MachineNetworkCIDR and ClusterNetworkCidr")
		}
	}
	err := VerifyCIDRsNotOverlap(serviceNetworkCidr, clusterNetworkCidr)
	if err != nil {
		return errors.Wrap(err, "ServiceNetworkCidr and ClusterNetworkCidr")
	}
	return nil
}

func VerifyNetworkHostPrefix(prefix int64) error {
	if prefix < 1 {
		return errors.Errorf("Host prefix, now %d, must be a positive integer", prefix)
	}
	return nil
}
