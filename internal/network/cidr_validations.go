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
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return err
	}
	if cidr.IP.IsUnspecified() {
		return errors.New("address must be specified")
	}
	nip := cidr.IP.Mask(cidr.Mask)
	if nip.String() != cidr.IP.String() {
		return errors.Errorf("invalid network address. got %s, expecting %s", cidr.String(), (&net.IPNet{IP: nip, Mask: cidr.Mask}).String())
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
	if prefix < 1 || prefix > 32 {
		return errors.Errorf("Network prefix %d is out of the allowed range (1 , 32)", prefix)
	}
	return nil
}
