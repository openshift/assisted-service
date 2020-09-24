package network

import (
	"net"

	"github.com/pkg/errors"
)

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
	ones, _ := cidr.Mask.Size()
	if ones < 1 || ones > 25 {
		return errors.New("Address mask size must be between 1 to 25 and must include at least 128 addresses")
	}
	if cidr.IP.IsUnspecified() {
		return errors.New("address must not be unspecified.  Unspecified address is the zero address (0.0.0.0)")
	}
	if ip.To4().String() != cidr.IP.To4().String() {
		return errors.Errorf("invalid network address. got %s, expecting %s", (&net.IPNet{IP: ip, Mask: cidr.Mask}).String(), cidr.String())
	}
	return nil
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func VerifyClusterCidrSize(hostNetworkPrefix int, clusterNetworkCIDR string, numberOfHosts int) error {
	_, cidr, err := net.ParseCIDR(clusterNetworkCIDR)
	if err != nil {
		return err
	}
	clusterNetworkPrefix, _ := cidr.Mask.Size()
	requestedNumHosts := max(4, numberOfHosts)
	possibleNumHosts := 1 << max(hostNetworkPrefix-clusterNetworkPrefix, 0)
	if requestedNumHosts > possibleNumHosts {
		return errors.Errorf("Cluster network CIDR prefix %d does not contain enough addresses for %d hosts each one with %d prefix (%d addresses)",
			clusterNetworkPrefix, requestedNumHosts, hostNetworkPrefix, 1<<(32-hostNetworkPrefix))
	}
	return nil
}

func VerifyClusterCIDRsNotOverlap(machineNetworkCidr, clusterNetworkCidr, serviceNetworkCidr string) error {
	err := VerifyCIDRsNotOverlap(machineNetworkCidr, serviceNetworkCidr)
	if err != nil {
		return errors.Wrap(err, "MachineNetworkCIDR and ServiceNetworkCIDR")
	}
	err = VerifyCIDRsNotOverlap(machineNetworkCidr, clusterNetworkCidr)
	if err != nil {
		return errors.Wrap(err, "MachineNetworkCIDR and ClusterNetworkCidr")
	}
	err = VerifyCIDRsNotOverlap(serviceNetworkCidr, clusterNetworkCidr)
	if err != nil {
		return errors.Wrap(err, "ServiceNetworkCidr and ClusterNetworkCidr")
	}
	return nil
}

func VerifyNetworkHostPrefix(prefix int64) error {
	if prefix < 1 || prefix > 25 {
		return errors.Errorf("Network prefix %d is out of the allowed range (1 , 25)", prefix)
	}
	return nil
}
