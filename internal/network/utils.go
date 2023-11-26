package network

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

func IsDefaultRoute(r *models.Route) (bool, error) {
	gw := net.ParseIP(r.Gateway)
	if gw == nil {
		return false, nil //gateway is empty for non default routes
	}

	dst := net.ParseIP(r.Destination)
	if dst == nil {
		return false, fmt.Errorf("unable to parse destination IP: %s", r.Destination)
	}

	return dst.IsUnspecified() && !gw.IsUnspecified(), nil
}

// Obtains the IP addresses used by a host
func GetInventoryIPAddresses(inventory *models.Inventory) ([]string, []string) {
	ipV4Addresses := []string{}
	ipV6Addresses := []string{}
	if inventory != nil {
		for i := range inventory.Interfaces {
			ipV4Addresses = append(ipV4Addresses, inventory.Interfaces[i].IPV4Addresses...)
			ipV6Addresses = append(ipV6Addresses, inventory.Interfaces[i].IPV6Addresses...)

		}
	}
	return ipV4Addresses, ipV6Addresses
}

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

func IsMachineNetworkRequired(cluster *common.Cluster) bool {
	return !swag.BoolValue(cluster.UserManagedNetworking) || common.IsSingleNodeCluster(cluster)
}

func IsMachineCidrAvailable(cluster *common.Cluster) bool {
	return common.IsSliceNonEmpty(cluster.MachineNetworks)
}

func GetMachineCidrById(cluster *common.Cluster, index int) string {
	if len(cluster.MachineNetworks) <= index {
		return ""
	} else {
		return string(cluster.MachineNetworks[index].Cidr)
	}
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

func GetMachineNetworkCidrs(cluster *common.Cluster) (ret []string) {
	for _, n := range cluster.MachineNetworks {
		ret = append(ret, string(n.Cidr))
	}
	return
}

func GetServiceNetworkCidrs(cluster *common.Cluster) (ret []string) {
	for _, n := range cluster.ServiceNetworks {
		ret = append(ret, string(n.Cidr))
	}
	return
}

func GetClusterNetworkCidrs(cluster *common.Cluster) (ret []string) {
	for _, n := range cluster.ClusterNetworks {
		ret = append(ret, string(n.Cidr))
	}
	return
}

func GetApiVips(cluster *common.Cluster) (ret []string) {
	if cluster.APIVips == nil {
		return nil
	}
	ret = funk.Map(cluster.APIVips, func(x *models.APIVip) string { return string(x.IP) }).([]string)
	return
}

func GetIngressVips(cluster *common.Cluster) (ret []string) {
	if cluster.IngressVips == nil {
		return nil
	}
	ret = funk.Map(cluster.IngressVips, func(x *models.IngressVip) string { return string(x.IP) }).([]string)
	return
}

func GetApiVipById(cluster *common.Cluster, index int) string {
	vips := GetApiVips(cluster)
	if len(vips) <= index {
		return ""
	} else {
		return vips[index]
	}
}

func GetIngressVipById(cluster *common.Cluster, index int) string {
	vips := GetIngressVips(cluster)
	if len(vips) <= index {
		return ""
	} else {
		return vips[index]
	}
}

func CidrToAddressFamily(cidr string) (AddressFamily, error) {
	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, err
	}
	if strings.Contains(cidr, ":") {
		return IPv6, nil
	}
	return IPv4, nil
}

func CidrsToAddressFamilies(cidrs []string) (ret []AddressFamily, err error) {
	for _, cidr := range cidrs {
		var af AddressFamily
		af, err = CidrToAddressFamily(cidr)
		if err != nil {
			return nil, err
		}
		ret = append(ret, af)
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i] < ret[j]
	})
	return
}

func GetClusterAddressFamilies(cluster *common.Cluster) ([]AddressFamily, error) {
	allCidrs := append(GetServiceNetworkCidrs(cluster), GetClusterNetworkCidrs(cluster)...)
	addressFamilies, err := CidrsToAddressFamilies(allCidrs)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(addressFamilies, func(i, j int) bool {
		return addressFamilies[i] < addressFamilies[j]
	})
	return CanonizeAddressFamilies(addressFamilies), nil
}

// Canonize slice of AddressFamily. It transforms multiple consecutive AddressFamily entries into a single entry
// while keeping the original order in the slice.
func CanonizeAddressFamilies(slice []AddressFamily) (ret []AddressFamily) {
	if len(slice) == 0 {
		return
	}
	ret = append(ret, slice[0])
	for i := 1; i != len(slice); i++ {
		if slice[i] != slice[i-1] {
			ret = append(ret, slice[i])
		}
	}
	return
}

type CidrInfo struct {
	Cidr          string
	InterfaceName string
}

func getHostNetworks(inventory *models.Inventory, getter func(i *models.Interface) []string) (ret []CidrInfo, err error) {
	toCidrSlice := func(interfaceName string, cidrs []string) (ret []CidrInfo) {
		for _, cidr := range cidrs {
			ret = append(ret, CidrInfo{
				Cidr:          cidr,
				InterfaceName: interfaceName,
			})
		}
		return
	}
	for _, intf := range inventory.Interfaces {
		var intfAddrs []string
		for _, cidr := range getter(intf) {
			var ipnet *net.IPNet
			if _, ipnet, err = net.ParseCIDR(cidr); err != nil {
				return
			}
			intfAddrs = append(intfAddrs, ipnet.String())
		}
		sort.Strings(intfAddrs)

		ret = append(ret, toCidrSlice(intf.Name, common.CanonizeStrings(intfAddrs))...)
	}
	return
}

func GetIPv4Networks(inventory *models.Inventory) (ret []CidrInfo, err error) {
	return getHostNetworks(inventory, func(i *models.Interface) []string { return i.IPV4Addresses })
}

func GetIPv6Networks(inventory *models.Inventory) (ret []CidrInfo, err error) {
	return getHostNetworks(inventory, func(i *models.Interface) []string { return i.IPV6Addresses })
}

func areListsEquivalent(len1, len2 int, areItemsEquivalent func(int, int) bool) bool {
	// Be aware! This function does not care about order of items on the list. For the sake of
	// comparison done here, [1, 2] and [2, 1] are equal. If order of entries is important, please
	// use another function.

	if len1 != len2 {
		return false
	}
	var usedIndexes []int
	containsEquivalentItem := func(index, length int) bool {
		for i := 0; i != length; i++ {
			if !funk.ContainsInt(usedIndexes, i) && areItemsEquivalent(index, i) {
				usedIndexes = append(usedIndexes, i)
				return true
			}
		}
		return false
	}
	for i := 0; i != len1; i++ {
		if !containsEquivalentItem(i, len1) {
			return false
		}
	}
	return true
}

func AreMachineNetworksIdentical(n1, n2 []*models.MachineNetwork) bool {
	return areListsEquivalent(len(n1), len(n2), func(i, j int) bool { return n1[i].Cidr == n2[j].Cidr })
}

func AreServiceNetworksIdentical(n1, n2 []*models.ServiceNetwork) bool {
	return areListsEquivalent(len(n1), len(n2), func(i, j int) bool { return n1[i].Cidr == n2[j].Cidr })
}

func AreClusterNetworksIdentical(n1, n2 []*models.ClusterNetwork) bool {
	return areListsEquivalent(len(n1), len(n2), func(i, j int) bool { return n1[i].Cidr == n2[j].Cidr && n1[i].HostPrefix == n2[j].HostPrefix })
}

func AreApiVipsIdentical(n1, n2 []*models.APIVip) bool {
	return areListsEquivalent(len(n1), len(n2), func(i, j int) bool { return n1[i].IP == n2[j].IP })
}

func AreIngressVipsIdentical(n1, n2 []*models.IngressVip) bool {
	return areListsEquivalent(len(n1), len(n2), func(i, j int) bool { return n1[i].IP == n2[j].IP })
}

func UpdateVipsTables(db *gorm.DB, cluster *common.Cluster, apiVipUpdated bool, ingressVipUpdated bool) error {
	if cluster == nil {
		return nil
	}
	if db == nil {
		return common.NewApiError(http.StatusInternalServerError, errors.New("DB error, tried to use empty DB connector"))
	}

	if apiVipUpdated {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := db.Where("cluster_id = ?", *cluster.ID).Delete(&models.APIVip{}).Error; err != nil {
				err = errors.Wrapf(err, "failed to delete api vips of cluster %s", *cluster.ID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
			for _, apiVip := range cluster.APIVips {
				apiVip.ClusterID = *cluster.ID
				if err := db.Save(apiVip).Error; err != nil {
					err = errors.Wrapf(err, "failed to update cluster apiVip %v of cluster %s", *apiVip, *cluster.ID)
					return common.NewApiError(http.StatusInternalServerError, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	if ingressVipUpdated {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := db.Where("cluster_id = ?", *cluster.ID).Delete(&models.IngressVip{}).Error; err != nil {
				err = errors.Wrapf(err, "failed to delete ingress vips of cluster %s", *cluster.ID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
			for _, ingressVip := range cluster.IngressVips {
				ingressVip.ClusterID = *cluster.ID
				if err := db.Save(ingressVip).Error; err != nil {
					err = errors.Wrapf(err, "failed to update cluster ingressVip %v of cluster %s", *ingressVip, *cluster.ID)
					return common.NewApiError(http.StatusInternalServerError, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}
