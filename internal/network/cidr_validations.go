package network

import (
	"errors"
	"fmt"
	"net"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// Minimum mask size to allow 128 addresses for cluster or service CIDRs
const MinMaskDelta = 7

// Minimum mask size for Machine CIDR to allow at least 16 addresses
const MinMachineMaskDelta = 4

// Minimum mask size for Machine CIDR to allow at least 2 addresses
const MinSNOMachineMaskDelta = 1

// When illegal CIDR is passed to net.ParseCIDR, the error message might be misleading in case the cidr is empty
func parseCIDR(cidr string) (ip net.IP, ipnet *net.IPNet, err error) {
	ip, ipnet, err = net.ParseCIDR(cidr)
	if err != nil {
		err = fmt.Errorf("Failed to parse CIDR '%s': %w", cidr, err)
	}
	return
}

func NetworksOverlap(aCidrStr, bCidrStr string) (bool, error) {
	_, acidr, err := parseCIDR(aCidrStr)
	if err != nil {
		return false, err
	}
	_, bcidr, err := parseCIDR(bCidrStr)
	if err != nil {
		return false, err
	}
	//overlapping occur if one of the CIDRs is a subset of the other
	return acidr.Contains(bcidr.IP) || bcidr.Contains(acidr.IP), nil
}

func VerifyNetworksNotOverlap(aCidrStr, bCidrStr models.Subnet) error {
	if aCidrStr == "" || bCidrStr == "" {
		return nil
	}

	overlap, err := NetworksOverlap(string(aCidrStr), string(bCidrStr))
	if err != nil {
		return err
	}

	if overlap {
		return fmt.Errorf("CIDRS %s and %s overlap", aCidrStr, bCidrStr)
	}
	return nil
}

func verifySubnetCIDR(cidrStr string, minSubnetMaskSize int) error {
	ip, cidr, err := parseCIDR(cidrStr)
	if err != nil {
		return err
	}
	ones, bits := cidr.Mask.Size()
	// We would like to allow enough addresses.  Therefore, ones must be not greater than (bits-minSubnetMaskSize)
	if ones < 1 || ones > bits-minSubnetMaskSize {
		return fmt.Errorf("Address mask size must be between 1 to %d and must include at least %d addresses", bits-minSubnetMaskSize, 1<<minSubnetMaskSize)
	}
	if cidr.IP.IsUnspecified() {
		return fmt.Errorf("The specified CIDR %s is invalid because its resulting routing prefix matches the unspecified address", cidrStr)
	}
	if !ip.Equal(cidr.IP) {
		return fmt.Errorf("%s is not a valid network CIDR", (&net.IPNet{IP: ip, Mask: cidr.Mask}).String())
	}
	return nil
}

func VerifyClusterOrServiceCIDR(cidrStr string) error {
	return verifySubnetCIDR(cidrStr, MinMaskDelta)
}

func VerifyMachineCIDR(cidrStr string, isSNO bool) error {
	maskDelta := MinMachineMaskDelta
	if isSNO {
		maskDelta = MinSNOMachineMaskDelta
	}
	return verifySubnetCIDR(cidrStr, maskDelta)
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
	_, cidr, err := parseCIDR(clusterNetworkCIDR)
	if err != nil {
		return err
	}
	clusterNetworkPrefix, bits := cidr.Mask.Size()
	// We would like to allow at least 128 addresses.  Therefore, hostNetworkPrefix must be not greater than (bits-7)
	if hostNetworkPrefix > bits-MinMaskDelta {
		return fmt.Errorf("Host prefix, now %d, must be less than or equal to %d to allow at least 128 addresses", hostNetworkPrefix, bits-MinMaskDelta)
	}
	var requestedNumHosts int
	if numberOfHosts == 1 {
		requestedNumHosts = 1
	} else {
		requestedNumHosts = max(4, numberOfHosts)
	}
	// 63 to avoid overflow
	possibleNumHosts := uint64(1) << min(63, max(hostNetworkPrefix-clusterNetworkPrefix, 0))
	if uint64(requestedNumHosts) > possibleNumHosts {
		return fmt.Errorf("Cluster network CIDR prefix %d does not contain enough addresses for %d hosts each one with %d prefix (%d addresses)",
			clusterNetworkPrefix, numberOfHosts, hostNetworkPrefix, uint64(1)<<min(63, bits-hostNetworkPrefix))
	}
	return nil
}

func VerifyNoNetworkCidrOverlaps(
	clusterNetworks []*models.ClusterNetwork,
	machineNetworks []*models.MachineNetwork,
	serviceNetworks []*models.ServiceNetwork) error {
	errs := []error{}
	for imn, mn := range machineNetworks {
		for _, cn := range clusterNetworks {
			if err := VerifyNetworksNotOverlap(mn.Cidr, cn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("%w (machine network and cluster network)", err))
			}
		}
		for _, sn := range serviceNetworks {
			if err := VerifyNetworksNotOverlap(mn.Cidr, sn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("%w (machine network and service network)", err))
			}
		}
		for _, omn := range machineNetworks[:imn] {
			if err := VerifyNetworksNotOverlap(mn.Cidr, omn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("invalid machine networks: %w", err))
			}
		}
	}
	for icn, cn := range clusterNetworks {
		for _, sn := range serviceNetworks {
			if err := VerifyNetworksNotOverlap(sn.Cidr, cn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("%w (service network and cluster network)", err))
			}
		}
		for _, ocn := range clusterNetworks[:icn] {
			if err := VerifyNetworksNotOverlap(cn.Cidr, ocn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("invalid cluster networks: %w", err))
			}
		}
	}
	for isn, sn := range serviceNetworks {
		for _, osn := range serviceNetworks[:isn] {
			if err := VerifyNetworksNotOverlap(sn.Cidr, osn.Cidr); err != nil {
				errs = append(errs, fmt.Errorf("invalid service networks: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

func VerifyNetworkHostPrefix(prefix int64) error {
	if prefix < 1 {
		return fmt.Errorf("Host prefix, now %d, must be a positive integer", prefix)
	}
	return nil
}

func isMachineNetworkCidrBigEnough(hosts []*models.Host, machineNetworkCidr string, log logrus.FieldLogger) bool {
	_, cidr, err := parseCIDR(machineNetworkCidr)
	if err != nil {
		log.WithError(err).Errorf("can't parse machine cidr %s", machineNetworkCidr)
		return true
	}

	networkPrefix, bits := cidr.Mask.Size()
	numOfHosts := len(hosts)
	if numOfHosts == 1 {
		//allow at least 2 addresses
		return networkPrefix <= bits-MinSNOMachineMaskDelta
	}

	//possible hosts in the range is the width of the range minus 2 network addresses minus 2 addresses for vips
	var availableAddresses int64 = (int64)(uint64(1)<<(bits-networkPrefix)) - int64(numOfHosts) - int64(4)
	return availableAddresses >= 0
}
