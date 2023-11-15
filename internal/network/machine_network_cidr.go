package network

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"

	"github.com/IBM/netaddr"
	"github.com/go-openapi/strfmt"
	"github.com/hashicorp/go-multierror"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func getVIPInterfaceNetwork(vip net.IP, addresses []string) *net.IPNet {
	for _, addr := range addresses {
		_, ipnet, err := net.ParseCIDR(addr)
		if err != nil {
			continue
		}
		if ipnet.Contains(vip) {
			return ipnet
		}
	}
	return nil
}

/*
 * Calculate the machine network CIDR from the one of (ApiVip, IngressVip) and the ip addresses of the hosts.
 * The ip addresses of the host appear with CIDR notation. Therefore, the network can be calculated from it.
 * The goal of this function is to find the first network that one of the vips belongs to it.
 * This network is returned as a result.
 */
func CalculateMachineNetworkCIDR(apiVip string, ingressVip string, hosts []*models.Host, isMatchRequired bool) (string, error) {
	var ip string
	if apiVip != "" {
		ip = apiVip
	} else if ingressVip != "" {
		ip = ingressVip
	} else {
		return "", nil
	}
	isIPv4 := IsIPv4Addr(ip)
	parsedVipAddr := net.ParseIP(ip)
	if parsedVipAddr == nil {
		return "", errors.Errorf("Could not parse VIP ip %s", ip)
	}
	for _, h := range hosts {
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			continue
		}
		for _, intf := range inventory.Interfaces {
			var ipnet *net.IPNet
			if isIPv4 {
				ipnet = getVIPInterfaceNetwork(parsedVipAddr, intf.IPV4Addresses)
			} else {
				ipnet = getVIPInterfaceNetwork(parsedVipAddr, intf.IPV6Addresses)
			}
			if ipnet != nil {
				return ipnet.String(), nil
			}
		}
	}
	if !isMatchRequired {
		return "", nil
	}
	return "", errors.Errorf("No suitable matching CIDR found for VIP %s", ip)
}

func ipInCidr(ipStr, cidrStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	_, ipnet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	return ipnet.Contains(ip)
}

func ipIsBroadcast(ipStr, cidrStr string) bool {
	// Broadcast addresses are only used for IPv4, so if this is not IPv4, don't do this check.
	if !IsIPv4Addr(ipStr) {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	_, ipnet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	broadcastAddress := netaddr.BroadcastAddr(ipnet)
	return ip.Equal(broadcastAddress)
}

func VerifyVipFree(hosts []*models.Host, vip string, machineNetworkCidr string, verification *models.VipVerification, log logrus.FieldLogger) models.VipVerification {
	if verification != nil {
		switch *verification {
		case models.VipVerificationSucceeded, models.VipVerificationFailed:
			return *verification
		default:
			// For all other cases (unverified / empty), fallback to checking if ip is in free list
			break
		}
	}
	return IpInFreeList(hosts, vip, machineNetworkCidr, log)
}

func VerifyVip(hosts []*models.Host, machineNetworkCidr string, vip string, vipName string, verification *models.VipVerification, log logrus.FieldLogger) (models.VipVerification, error) {
	if machineNetworkCidr == "" {
		return models.VipVerificationUnverified, errors.Errorf("%s <%s> cannot be set if Machine Network CIDR is empty", vipName, vip)
	}
	if !ipInCidr(vip, machineNetworkCidr) {
		return models.VipVerificationFailed, errors.Errorf("%s <%s> does not belong to machine-network-cidr <%s>", vipName, vip, machineNetworkCidr)
	}
	if ipIsBroadcast(vip, machineNetworkCidr) {
		return models.VipVerificationFailed, errors.Errorf("%s <%s> is the broadcast address of machine-network-cidr <%s>", vipName, vip, machineNetworkCidr)
	}
	var msg string
	ret := VerifyVipFree(hosts, vip, machineNetworkCidr, verification, log)
	switch ret {
	case models.VipVerificationSucceeded:
		return ret, nil
	case models.VipVerificationFailed:
		msg = fmt.Sprintf("%s <%s> is already in use in cidr %s", vipName, vip, machineNetworkCidr)
		//In that particular case verify that the machine network range is big enough
		//to accommodates hosts and vips
		if !isMachineNetworkCidrBigEnough(hosts, machineNetworkCidr, log) {
			msg = fmt.Sprintf("%s. The machine network range is too small for the cluster. Please redefine the network.", msg)
		}
	case models.VipVerificationUnverified:
		msg = fmt.Sprintf("%s <%s> are not verified yet.", vipName, vip)
	}
	return ret, errors.New(msg)
}

func ValidateNoVIPAddressesDuplicates(apiVips []*models.APIVip, ingressVips []*models.IngressVip) error {
	var (
		err                     error
		multiErr                error
		seenApiVipAddresses     = make(map[string]bool)
		seenIngressVipAddresses = make(map[string]bool)
	)
	for i := range apiVips {
		ipAddress := string(apiVips[i].IP)
		if ipAddress == "" {
			continue
		}
		_, found := seenApiVipAddresses[ipAddress]
		if found {
			err = errors.Errorf("The IP address \"%s\" appears multiple times in apiVIPs", ipAddress)
			multiErr = multierror.Append(multiErr, err)
		} else {
			seenApiVipAddresses[ipAddress] = true
		}
	}

	for i := range ingressVips {
		ipAddress := string(ingressVips[i].IP)
		if ipAddress == "" {
			continue
		}
		_, found := seenIngressVipAddresses[ipAddress]
		if found {
			err = errors.Errorf("The IP address \"%s\" appears multiple times in ingressVIPs", ipAddress)
			multiErr = multierror.Append(multiErr, err)
		} else {
			seenIngressVipAddresses[ipAddress] = true
		}
		// Should also assert for duplicates between Ingress VIPs and API VIPs
		_, found = seenApiVipAddresses[ipAddress]
		if found {
			err = errors.Errorf("The IP address \"%s\" appears both in apiVIPs and ingressVIPs", ipAddress)
			multiErr = multierror.Append(multiErr, err)
		}
	}

	if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
		return multiErr
	}
	return nil
}

// This function is called from places which assume it is OK for a VIP to be unverified.
// The assumption is that VIPs are eventually verified by cluster validation
// (i.e api-vips-valid, ingress-vips-valid)
func VerifyVips(hosts []*models.Host, machineNetworkCidr string, apiVip string, ingressVip string, log logrus.FieldLogger) error {
	verification, err := VerifyVip(hosts, machineNetworkCidr, apiVip, "api-vip", nil, log)
	// Error is ignored if the verification didn't fail
	if verification != models.VipVerificationFailed {
		verification, err = VerifyVip(hosts, machineNetworkCidr, ingressVip, "ingress-vip", nil, log)
	}
	if verification != models.VipVerificationFailed {
		return ValidateNoVIPAddressesDuplicates([]*models.APIVip{{IP: models.IP(apiVip)}}, []*models.IngressVip{{IP: models.IP(ingressVip)}})
	}
	return err
}

func findMatchingIPForFamily(ipnet *net.IPNet, addresses []string) (bool, string) {
	for _, addr := range addresses {
		ip, _, err := net.ParseCIDR(addr)
		if err != nil {
			continue
		}
		if ipnet.Contains(ip) {
			return true, addr
		}
	}
	return false, ""
}

func findMatchingIP(ipnet *net.IPNet, intf *models.Interface, isIPv4 bool) (bool, string) {
	if isIPv4 {
		return findMatchingIPForFamily(ipnet, intf.IPV4Addresses)
	} else {
		return findMatchingIPForFamily(ipnet, intf.IPV6Addresses)
	}
}

func getMachineCIDRObj(host *models.Host, machineNetworkCidr string, obj string) (string, error) {
	var inventory models.Inventory
	var err error
	isIPv4 := IsIPV4CIDR(machineNetworkCidr)
	if err = json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return "", err
	}
	_, ipNet, err := net.ParseCIDR(machineNetworkCidr)
	if err != nil {
		return "", err
	}
	for _, intf := range inventory.Interfaces {
		found, addr := findMatchingIP(ipNet, intf, isIPv4)
		if found {
			switch obj {
			case "interface":
				return intf.Name, nil
			case "ip":
				return strings.Split(addr, "/")[0], nil
			default:
				return "", errors.Errorf("obj %s not supported", obj)
			}
		}
	}
	return "", errors.Errorf("No matching interface found for host %s", host.ID.String())
}

func GetPrimaryMachineCIDRInterface(host *models.Host, cluster *common.Cluster) (string, error) {
	primaryMachineCidr := ""
	if IsMachineCidrAvailable(cluster) {
		primaryMachineCidr = GetMachineCidrById(cluster, 0)
	}
	return getMachineCIDRObj(host, primaryMachineCidr, "interface")
}

func GetPrimaryMachineCIDRIP(host *models.Host, cluster *common.Cluster) (string, error) {
	primaryMachineCidr := ""
	if IsMachineCidrAvailable(cluster) {
		primaryMachineCidr = GetMachineCidrById(cluster, 0)
	}
	return getMachineCIDRObj(host, primaryMachineCidr, "ip")
}

func IpInCidr(ipAddr, cidr string) (bool, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}
	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return false, errors.New("IP is nil")
	}
	return ipNet.Contains(ip), nil
}

func belongsToNetwork(log logrus.FieldLogger, h *models.Host, machineIpnet *net.IPNet) bool {
	var inventory models.Inventory
	err := json.Unmarshal([]byte(h.Inventory), &inventory)
	if err != nil {
		log.WithError(err).Warnf("Error unmarshalling host %s inventory %s", h.ID, h.Inventory)
		return false
	}
	isIPv4 := IsIPV4CIDR(machineIpnet.String())
	for _, intf := range inventory.Interfaces {
		if found, _ := findMatchingIP(machineIpnet, intf, isIPv4); found {
			return true
		}
	}
	return false
}

func GetPrimaryMachineCIDRHosts(log logrus.FieldLogger, cluster *common.Cluster) ([]*models.Host, error) {
	if !IsMachineCidrAvailable(cluster) {
		return nil, errors.New("Machine network CIDR was not set in cluster")
	}
	_, machineIpnet, err := net.ParseCIDR(GetMachineCidrById(cluster, 0))
	if err != nil {
		return nil, err
	}
	ret := make([]*models.Host, 0)
	for _, h := range cluster.Hosts {
		if belongsToNetwork(log, h, machineIpnet) {
			ret = append(ret, h)
		}
	}
	return ret, nil
}

// GetPrimaryMachineCidrForUserManagedNetwork used to get the primary machine cidr in case of none platform and sno
func GetPrimaryMachineCidrForUserManagedNetwork(cluster *common.Cluster, log logrus.FieldLogger) string {
	if IsMachineCidrAvailable(cluster) {
		return GetMachineCidrById(cluster, 0)
	}

	bootstrap := common.GetBootstrapHost(cluster)
	if bootstrap == nil {
		log.Warnf("No bootstrap found in cluster %s", cluster.ID)
		return ""
	}

	networks := GetInventoryNetworks([]*models.Host{bootstrap}, log)
	if len(networks) > 0 {
		// if there is ipv4 network, return it or return the first one
		for _, network := range networks {
			if IsIPV4CIDR(network) {
				return network
			}
		}
		return networks[0]
	}
	return ""
}

func GetSecondaryMachineCidr(cluster *common.Cluster) (string, error) {
	var secondaryMachineCidr string

	if IsMachineCidrAvailable(cluster) {
		secondaryMachineCidr = GetMachineCidrById(cluster, 1)
	}
	if secondaryMachineCidr != "" {
		return secondaryMachineCidr, nil
	}
	return "", errors.Errorf("unable to get secondary machine network for cluster %s", cluster.ID.String())
}

// GetMachineNetworksFromBoostrapHost used to collect machine networks from the cluster's bootstrap host.
// The function will get at most one IPv4 and one IPv6 network.
func GetMachineNetworksFromBoostrapHost(cluster *common.Cluster, log logrus.FieldLogger) []*models.MachineNetwork {
	if IsMachineCidrAvailable(cluster) {
		return cluster.MachineNetworks
	}

	bootstrap := common.GetBootstrapHost(cluster)
	if bootstrap == nil {
		log.Warnf("No bootstrap found in cluster %s", cluster.ID)
		return []*models.MachineNetwork{}
	}

	networks := GetInventoryNetworks([]*models.Host{bootstrap}, log)
	var v4net, v6net string
	res := []*models.MachineNetwork{}
	if len(networks) > 0 {
		for _, network := range networks {
			if IsIPV4CIDR(network) && v4net == "" {
				v4net = network
			}
			if IsIPv6CIDR(network) && v6net == "" {
				v6net = network
			}
		}
	}

	if v4net != "" {
		res = append(res, &models.MachineNetwork{Cidr: models.Subnet(v4net)})
	}
	if v6net != "" {
		res = append(res, &models.MachineNetwork{Cidr: models.Subnet(v6net)})
	}

	return res
}

func GetIpForSingleNodeInstallation(cluster *common.Cluster, log logrus.FieldLogger) (string, error) {
	bootstrap := common.GetBootstrapHost(cluster)
	if bootstrap == nil {
		return "", errors.Errorf("no bootstrap host were found in cluster")
	}
	cidr := GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
	hostIp, err := getMachineCIDRObj(bootstrap, cidr, "ip")
	if hostIp == "" || err != nil {
		msg := "failed to get ip for single node installation"
		if err != nil {
			msg = errors.Wrapf(err, msg).Error()
		}
		return "", errors.Errorf(msg)
	}

	return hostIp, nil
}

func GetInventoryNetworks(hosts []*models.Host, log logrus.FieldLogger) []string {
	var err error
	cidrs := make(map[string]bool)
	for _, h := range hosts {
		if h.Inventory != "" {
			var inventory models.Inventory
			err = json.Unmarshal([]byte(h.Inventory), &inventory)
			if err != nil {
				log.WithError(err).Warnf("Unmarshal inventory %s", h.Inventory)
				continue
			}
			for _, inf := range inventory.Interfaces {

				for _, ipv4 := range inf.IPV4Addresses {
					_, cidr, err := net.ParseCIDR(ipv4)
					if err != nil {
						log.WithError(err).Warnf("Parse CIDR %s", ipv4)
						continue
					}
					cidrs[cidr.String()] = true
				}

				for _, ipv6 := range inf.IPV6Addresses {
					_, cidr, err := net.ParseCIDR(ipv6)
					if err != nil {
						log.WithError(err).Warnf("Parse CIDR %s", ipv6)
						continue
					}
					cidrs[cidr.String()] = true
				}
			}
		}
	}
	ret := make([]string, 0)
	for cidr := range cidrs {
		ret = append(ret, cidr)
	}
	return ret
}

func GetInventoryNetworksByFamily(hosts []*models.Host, log logrus.FieldLogger) (map[AddressFamily][]string, error) {
	networks := GetInventoryNetworks(hosts, log)
	ret := make(map[AddressFamily][]string)
	for _, n := range networks {
		family, err := CidrToAddressFamily(n)
		if err != nil {
			return nil, err
		}
		ret[family] = append(ret[family], n)
	}
	return ret, nil
}

func GetDefaultRouteNetworkByFamily(h *models.Host, networks map[AddressFamily][]string, log logrus.FieldLogger) (map[AddressFamily]string, error) {
	var err error
	ret := make(map[AddressFamily]string)

	//start with dummy route
	defaultRoutev4 := &models.Route{Metric: math.MaxInt32}
	defaultRoutev6 := &models.Route{Metric: math.MaxInt32}

	if h.Inventory != "" {
		var inventory models.Inventory
		err = json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			log.WithError(err).Warnf("Unmarshal inventory %s", h.Inventory)
			return ret, err
		}
		//find the minimal default route
		var isDefault, found bool
		for _, route := range inventory.Routes {
			if isDefault, err = IsDefaultRoute(route); err != nil {
				log.WithError(err).Errorf("fail to find default route")
				return ret, err
			}
			if isDefault {
				if IsIPv4Addr(route.Gateway) && route.Metric < defaultRoutev4.Metric {
					defaultRoutev4 = route
					found = true
				} else if IsIPv6Addr(route.Gateway) && route.Metric < defaultRoutev6.Metric {
					defaultRoutev6 = route
					found = true
				}
			}
		}

		if !found {
			return ret, fmt.Errorf("could not find default route")
		}

		//if the default route gateway is located in one of the cidrs
		//in the inventory networks then return this network (per family)
		for _, cidr := range networks[IPv4] {
			if ipInCidr(defaultRoutev4.Gateway, cidr) {
				ret[IPv4] = cidr
				break
			}
		}
		for _, cidr := range networks[IPv6] {
			if ipInCidr(defaultRoutev6.Gateway, cidr) {
				ret[IPv6] = cidr
				break
			}
		}
		log.Infof("available default route CIDRs: %+v", ret)
		return ret, nil
	}
	return ret, fmt.Errorf("can not find cidr by route: no inventory for host %s", h.ID.String())
}

func IsHostInPrimaryMachineNetCidr(log logrus.FieldLogger, cluster *common.Cluster, host *models.Host) bool {
	// The host should belong to all the networks specified as Machine Networks.

	// TODO(mko) This rule should be revised as soon as OCP supports multiple machineNetwork
	//           entries using the same IP stack.

	if !IsMachineCidrAvailable(cluster) {
		return false
	}

	ret := true
	for _, machineNet := range cluster.MachineNetworks {
		_, machineIpnet, err := net.ParseCIDR(string(machineNet.Cidr))
		if err != nil {
			return false
		}
		ret = ret && belongsToNetwork(log, host, machineIpnet)
	}
	return ret
}

type IPSet map[strfmt.IPv4]struct{}

func (s IPSet) Add(str strfmt.IPv4) {
	s[str] = struct{}{}
}

func (s IPSet) Intersect(other IPSet) IPSet {
	ret := make(IPSet)
	for k := range s {
		if v, ok := other[k]; ok {
			ret[k] = v
		}
	}
	return ret
}

func freeAddressesUnmarshal(network, freeAddressesStr string, prefix *string) (IPSet, error) {
	var unmarshaled models.FreeNetworksAddresses
	err := json.Unmarshal([]byte(freeAddressesStr), &unmarshaled)
	if err != nil {
		return nil, err
	}
	for _, f := range unmarshaled {
		if f.Network == network {
			ret := make(IPSet)
			for _, a := range f.FreeAddresses {
				if prefix == nil || strings.HasPrefix(a.String(), *prefix) {
					ret.Add(a)
				}
			}
			return ret, nil
		}
	}
	return nil, errors.Errorf("No network %s found", network)
}

func MakeFreeAddressesSet(hosts []*models.Host, network string, prefix *string, log logrus.FieldLogger) IPSet {
	var (
		availableFreeAddresses []string
		sets                   = make([]IPSet, 0)
		resultingSet           = make(IPSet)
	)
	for _, h := range hosts {
		if h.FreeAddresses != "" {
			availableFreeAddresses = append(availableFreeAddresses, h.FreeAddresses)
		}
	}
	if len(availableFreeAddresses) == 0 {
		return resultingSet
	}
	// Create IP sets from each of the hosts free-addresses
	for _, a := range availableFreeAddresses {
		s, err := freeAddressesUnmarshal(network, a, prefix)
		if err != nil {
			log.WithError(err).Debugf("Unmarshal free addresses for network %s", network)
			continue
		}
		// TODO: Have to decide if we want to filter empty sets
		sets = append(sets, s)
	}
	if len(sets) == 0 {
		return resultingSet
	}

	// Perform set intersection between all valid sets
	resultingSet = sets[0]
	for _, s := range sets[1:] {
		resultingSet = resultingSet.Intersect(s)
	}
	return resultingSet
}

// This is best effort validation.  Therefore, validation will be done only if there are IPs in free list
func IpInFreeList(hosts []*models.Host, vipIPStr, network string, log logrus.FieldLogger) models.VipVerification {
	freeSet := MakeFreeAddressesSet(hosts, network, nil, log)
	if len(freeSet) > 0 {
		_, isFree := freeSet[strfmt.IPv4(vipIPStr)]
		if isFree {
			return models.VipVerificationSucceeded
		}
		return models.VipVerificationFailed
	}
	// If there is no free list for the network, the VIP is unverified, assuming that free list did not arrive yet.
	return models.VipVerificationUnverified
}

func CreateMachineNetworksArray(machineCidr string) []*models.MachineNetwork {
	if machineCidr == "" {
		return []*models.MachineNetwork{}
	}
	return []*models.MachineNetwork{{Cidr: models.Subnet(machineCidr)}}
}
