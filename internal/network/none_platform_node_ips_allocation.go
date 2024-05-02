package network

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// When none platform is in use, if there is ambiguity in node-ip assignment, then incorrect assignment might lead
// to installation failure.  This happens when etcd detects that the socket address from an etcd node does not match
// the expected address in the peer certificate. In this case etcd rejects such connection.
// Example: assuming two networks - net1 and net2.
// master node 1 has 1 address that belongs to net1.
// master node 2 has 2 addresses.  one that belongs to net 1, and another that belongs to net 2
// master node 3 has 1 address that belongs to net 1.
// If the selected node-ip of master node 2 belongs to net 2, then when it will create a connection with any other master node,
// the socket address will be the address that belongs to net 1. Since etcd expects it to be the same as the node-ip, it will
// reject the connection.
// A similar situation may happen with tunnel IPs (Geneve for OVN).  When setting tunnel, OVS configuration contains the
// node-ips as endpoints.  When there is a collision, the receiving endpoint of a tunnel may receive a tunnel packet
// from an IP address it does expect.  This leads OVS to ignore the packet.  Such situation may occur for master and worker
// nodes.  If such collision happens for workers, there is a possibility that the cluster will still be functional.
//
// This file attempts to solve the issue automatically.

// NodeIpAllocation is the allocation result for specific host
type NodeIpAllocation struct {
	NodeIp string
	HintIp string
	Cidr   string
}

// nodeIpCandidate is an assignment candidate which is used when attempting to find optimal solution
type nodeIpCandidate struct {
	hostId strfmt.UUID
	ip     net.IP
}

// hostNetwork represents discovered network in a host
type hostNetwork struct {
	hostId strfmt.UUID
	ipNet  *net.IPNet
}

// eligibleNodeIp is an ip with its network which eligible for allocation.  It means the IP is connected to all other hosts
// and its interface has default route
type eligibleNodeIp struct {
	nodeIp      net.IP
	ipNet       *net.IPNet
	routeMetric int32
}

// hostCandidates gathers all eligible node IPs and all the discovered networks in that host
type hostCandidates struct {
	hostId          strfmt.UUID
	hostNetworks    []*hostNetwork
	eligibleNodeIps []*eligibleNodeIp
}

// allocationSolution contains all or partial set of the allocation.
type allocationSolution struct {
	routeMetric       int32
	nodeIpAllocations map[strfmt.UUID]*NodeIpAllocation
}

func getDefaultMetricForInterface(interfaceName string, routes []*models.Route, isIpv4 bool) (bool, int32) {
	for _, r := range routes {
		if isIpv4 && r.Family != unix.AF_INET || !isIpv4 && r.Family != unix.AF_INET6 {
			continue
		}
		isDefault, err := IsDefaultRoute(r)
		if err != nil {
			continue
		}
		if isDefault && r.Interface == interfaceName {
			return true, r.Metric
		}
	}
	return false, 0
}

// Create candidates for host.  For an address to be considered as eligible IP, it has to be connected, and its interface must
// have default route.
func createHostCandidates(host *models.Host, connectivity *Connectivity, isIpv4 bool) (*hostCandidates, error) {
	connectedAddresses := connectivity.L3ConnectedAddresses[lo.FromPtr(host.ID)]
	if len(connectedAddresses) == 0 {
		return nil, errors.Errorf("no address found for host %s", lo.FromPtr(host.ID))
	}
	connectedIps := lo.Map(lo.Filter(connectedAddresses, func(addrStr string, _ int) bool { return isIpv4 == IsIPv4Addr(addrStr) }),
		func(addrStr string, _ int) net.IP { return net.ParseIP(addrStr) })
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal inventory for host %s", lo.FromPtr(host.ID))
	}
	var (
		eligibleNodeIps []*eligibleNodeIp
		hostNetworks    []*hostNetwork
	)

	for _, intf := range inventory.Interfaces {
		addresses := intf.IPV4Addresses
		if !isIpv4 {
			addresses = intf.IPV6Addresses
		}
		hasDefaultMetric, metric := getDefaultMetricForInterface(intf.Name, inventory.Routes, isIpv4)
		for _, addrStr := range addresses {
			addrIp, ipNet, err := net.ParseCIDR(addrStr)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse address %s for host %s", addrStr, lo.FromPtr(host.ID))
			}
			hostNetworks = append(hostNetworks, &hostNetwork{
				hostId: *host.ID,
				ipNet:  ipNet,
			})
			if hasDefaultMetric && lo.ContainsBy(connectedIps, func(ip net.IP) bool { return ip.Equal(addrIp) }) {
				eligibleNodeIps = append(eligibleNodeIps, &eligibleNodeIp{
					nodeIp:      addrIp,
					ipNet:       ipNet,
					routeMetric: metric,
				})
			}
		}
	}
	if len(eligibleNodeIps) == 0 {
		return nil, errors.Errorf("failed to create candidates for host %s", *host.ID)
	}
	return &hostCandidates{
		hostId:          lo.FromPtr(host.ID),
		hostNetworks:    hostNetworks,
		eligibleNodeIps: eligibleNodeIps,
	}, nil
}

func createHostsCandidates(hosts []*models.Host, connectivity *Connectivity, isIpv4 bool) (candidates []*hostCandidates, err error) {
	for _, h := range hosts {
		c, err := createHostCandidates(h, connectivity, isIpv4)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return
}

type collisionDetector struct {
	assignedNodeIps    []*nodeIpCandidate
	prohibitedNetworks []*hostNetwork
}

func newCollisionDetector() *collisionDetector {
	return &collisionDetector{}
}
func newCollisionDetectorForNodeIp(hostCandidates *hostCandidates, eligibleNodeIp *eligibleNodeIp) *collisionDetector {
	// All host networks that the eligible Node IP does not belong to, are considered prohibited networks.  It means
	// that node-ip allocation on these networks should be avoided
	prohibitedNetworks := lo.Filter(hostCandidates.hostNetworks,
		func(hostNetwork *hostNetwork, _ int) bool {
			return hostNetwork.ipNet.String() != eligibleNodeIp.ipNet.String()
		})
	return &collisionDetector{
		assignedNodeIps: []*nodeIpCandidate{{
			hostId: hostCandidates.hostId,
			ip:     eligibleNodeIp.nodeIp,
		}},
		prohibitedNetworks: prohibitedNetworks,
	}
}

func (c *collisionDetector) detectIpNetworkCollision(nodeIpCandidate *nodeIpCandidate, prohibitedNetworks []*hostNetwork) []error {
	var errs []error
	for _, prohibitedNetwork := range prohibitedNetworks {
		if prohibitedNetwork.ipNet.Contains(nodeIpCandidate.ip) {
			errs = append(errs,
				errors.Errorf("IP assignment %s in host %s collides with prohibited network %s in host %s",
					nodeIpCandidate.ip, nodeIpCandidate.hostId, prohibitedNetwork.ipNet.String(), prohibitedNetwork.hostId))
		}
	}
	return errs
}

func (c *collisionDetector) detectIpsNetworksCollisions(assignedNodeIps []*nodeIpCandidate, prohibitedNetworks []*hostNetwork) []error {
	var errs []error
	for _, assignment := range assignedNodeIps {
		errs = append(errs, c.detectIpNetworkCollision(assignment, prohibitedNetworks)...)
	}
	return errs
}

func (c *collisionDetector) detectCollisions(other *collisionDetector) []error {
	var errs []error
	errs = append(append(errs,
		c.detectIpsNetworksCollisions(c.assignedNodeIps, other.prohibitedNetworks)...),
		c.detectIpsNetworksCollisions(other.assignedNodeIps, c.prohibitedNetworks)...)
	return errs
}

func (c *collisionDetector) merge(other *collisionDetector) *collisionDetector {
	return &collisionDetector{
		assignedNodeIps:    append(c.assignedNodeIps, other.assignedNodeIps...),
		prohibitedNetworks: append(c.prohibitedNetworks, other.prohibitedNetworks...),
	}
}

func (c *collisionDetector) String() string {
	return fmt.Sprintf("collision detector - assigned-node-ips [%s] prohibited-networks: [%s]",
		strings.Join(lo.Map(c.assignedNodeIps, func(n *nodeIpCandidate, _ int) string { return n.ip.String() }), ","),
		strings.Join(lo.Map(c.prohibitedNetworks, func(h *hostNetwork, _ int) string { return h.ipNet.String() }), ","))
}

type nodeIpAllocator struct {
	candidates []*hostCandidates
	clusterId  strfmt.UUID
	log        logrus.FieldLogger
}

func (n *nodeIpAllocator) sort(allocations []*allocationSolution) {
	sort.SliceStable(allocations, func(i, j int) bool {
		return allocations[i].routeMetric < allocations[j].routeMetric
	})
}

// Allocate node IPs.  Since the node IPs are dependent on each other, then all of them must be allocated
// through this allocation procedure.  It tries to find an allocation when no conflict exists.
func (n *nodeIpAllocator) allocate(index int, parentContext *collisionDetector) (map[strfmt.UUID]*NodeIpAllocation, []error) {
	if index == len(n.candidates) {
		return make(map[strfmt.UUID]*NodeIpAllocation), nil
	}
	var (
		errs                []error
		allocationSolutions []*allocationSolution
		h                   = n.candidates[index]
	)
	for i := range h.eligibleNodeIps {
		c := h.eligibleNodeIps[i]
		localCollisionContext := newCollisionDetectorForNodeIp(h, c)
		n.log.Debugf("local collision detector for host %s: %s", h.hostId, localCollisionContext)
		if collisionErrs := localCollisionContext.detectCollisions(parentContext); len(collisionErrs) > 0 {
			errs = append(errs, collisionErrs...)
			continue
		}
		result, childErrs := n.allocate(index+1, parentContext.merge(localCollisionContext))
		if len(childErrs) > 0 {
			errs = append(errs, childErrs...)
			continue
		}
		result[h.hostId] = &NodeIpAllocation{
			NodeIp: c.nodeIp.String(),
			HintIp: c.ipNet.IP.String(),
			Cidr:   c.ipNet.String(),
		}
		allocationSolutions = append(allocationSolutions, &allocationSolution{
			routeMetric:       c.routeMetric,
			nodeIpAllocations: result,
		})
	}
	if len(allocationSolutions) > 0 {
		n.sort(allocationSolutions)
		return allocationSolutions[0].nodeIpAllocations, nil
	}
	return nil, errs
}

func (n *nodeIpAllocator) allocateNodeIps() (map[strfmt.UUID]*NodeIpAllocation, error) {
	result, errs := n.allocate(0, newCollisionDetector())
	uniqErrs := lo.UniqBy(errs, func(e error) string { return e.Error() })
	err := stderrors.Join(uniqErrs...)
	if err != nil {
		n.log.WithError(err).Errorf("failed to allocate master node ips for cluster %s", n.clusterId)
		return nil, err
	}
	return result, nil
}

func GenerateNonePlatformAddressAllocation(cluster *common.Cluster, log logrus.FieldLogger) (map[strfmt.UUID]*NodeIpAllocation, error) {
	addressFamilies, err := GetClusterAddressFamilies(cluster)
	if err != nil {
		return nil, errors.Wrapf(err, "failed tp get address families for cluster %s", lo.FromPtr(cluster.ID))
	}
	isIpv4 := lo.Contains(addressFamilies, IPv4)
	isIpv6 := lo.Contains(addressFamilies, IPv6)
	if !(isIpv4 || isIpv6) {
		return nil, errors.Errorf("No address family found for cluster %s", lo.FromPtr(cluster.ID))
	}

	if cluster.ConnectivityMajorityGroups == "" {
		return nil, errors.Errorf("connectivity majority groups are missing for cluster %s", lo.FromPtr(cluster.ID))
	}
	var connectivity Connectivity
	err = json.Unmarshal([]byte(cluster.ConnectivityMajorityGroups), &connectivity)
	if err != nil {
		return nil, errors.Errorf("failed to unmarshal majority groups for cluster %s", lo.FromPtr(cluster.ID))
	}
	if len(cluster.Hosts) != len(connectivity.L3ConnectedAddresses) {
		return nil, errors.Errorf("not all the hosts are connected.  Skipping address allocation for cluster %s", lo.FromPtr(cluster.ID))
	}
	candidates, err := createHostsCandidates(cluster.Hosts, &connectivity, isIpv4)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create hosts candidates for cluster %s", lo.FromPtr(cluster.ID))
	}
	allocator := &nodeIpAllocator{
		candidates: candidates,
		log:        log,
		clusterId:  lo.FromPtr(cluster.ID),
	}
	allocations, err := allocator.allocateNodeIps()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to perform node ips allocation for cluster %s", lo.FromPtr(cluster.ID))
	}
	return allocations, nil
}
