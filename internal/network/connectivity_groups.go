package network

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"

	"github.com/go-openapi/strfmt"
	"github.com/golang-collections/go-datastructures/bitarray"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

type AddressFamily int

const (
	IPv4 AddressFamily = 4
	IPv6 AddressFamily = 6
)

func (a AddressFamily) String() string {
	switch a {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	default:
		return fmt.Sprintf("Unexpected family value %d", a)
	}
}

type connectivityKey struct {
	first, second int
}

type connectivityValue struct {
	first2second, second2first bool
}

// Map for indicating if there is mutual connectivity between 2 hosts
type connectivityMap map[connectivityKey]*connectivityValue

func makeKey(from, to int) connectivityKey {
	if from < to {
		return connectivityKey{first: from, second: to}
	} else {
		return connectivityKey{first: to, second: from}
	}
}

func (c connectivityMap) add(from, to int, connected bool) {
	key := makeKey(from, to)
	value, ok := c[key]
	if !ok {
		value = &connectivityValue{}
		c[key] = value
	}
	if from == key.first {
		value.first2second = connected
	} else {
		value.second2first = connected
	}
}

func (c connectivityMap) isConnected(from, to int) bool {
	value, ok := c[makeKey(from, to)]
	return ok && value.first2second && value.second2first
}

/*
 * connectivitySet is used to indicate that there is connectivity between at least one of the set members to the other members.
 * The actual meaning of the specific instance depends on the context.
 */
type connectivitySet struct {
	array   bitarray.BitArray
	groupId groupId
}

func NewConnectivitySet(size int) *connectivitySet {
	return &connectivitySet{array: bitarray.NewBitArray(uint64(size))}
}

func (c *connectivitySet) add(item int) error {
	return c.array.SetBit(uint64(item))
}

func (c *connectivitySet) intersect(other *connectivitySet) *connectivitySet {
	return &connectivitySet{
		array: c.array.And(other.array),
	}
}

func (c *connectivitySet) containsElement(id int) bool {
	b, err := c.array.GetBit(uint64(id))
	return err == nil && b
}

func (c *connectivitySet) equals(other *connectivitySet) bool {
	return c.array.Equals(other.array)
}

func (c *connectivitySet) isSupersetOf(other *connectivitySet) bool {
	intersection := c.intersect(other)
	return intersection.equals(other)
}

func (c *connectivitySet) id() groupId {
	if c.groupId == nil {
		c.groupId = c.array.ToNums()
	}
	return c.groupId
}

func (c *connectivitySet) toList(hosts []*models.Host) []strfmt.UUID {
	ret := make([]strfmt.UUID, 0)
	for _, k := range c.id() {
		ret = append(ret, *hosts[k].ID)
	}
	return ret
}

func (c *connectivitySet) len() int {
	return len(c.id())
}

// groupId is used for unique sorting of a list containing connectivitySet elements
type groupId []uint64

func (g groupId) isLess(other groupId) bool {
	if len(g) != len(other) {
		return len(g) > len(other)
	}
	index := 0
	for ; index != len(g) && g[index] == other[index]; index++ {
	}
	return index < len(g) && g[index] < other[index]
}

// Auxiliary type to find out if the contained set has a full mesh connectivity
type connectivityGroup struct {
	set   *connectivitySet
	count int
}

// Unique list of connectivity group elements.  The uniqueness is by the set elements
type connectivityGroupList struct {
	groups []*connectivityGroup
}

func (c *connectivityGroupList) containsSet(cs *connectivitySet) bool {
	for _, group := range c.groups {
		if group.set.equals(cs) {
			return true
		}
	}
	return false
}

func (c *connectivityGroupList) addSet(cs *connectivitySet) {
	if !c.containsSet(cs) {
		c.groups = append(c.groups, &connectivityGroup{
			set:   cs,
			count: 0,
		})
	}
}

// Group candidate is the connectivity view of a single host (me)
type groupCandidate struct {
	set *connectivitySet
	me  int
}

func createGroupList(groupCandidates []groupCandidate) connectivityGroupList {
	var groupList connectivityGroupList

	// First iteration - gather the sets
	for _, candidate := range groupCandidates {

		// All sets pending for insertion
		pendingSets := make([]*connectivitySet, 0)

		// Add the set of the current candidate to the pending list
		pendingSets = append(pendingSets, candidate.set)
		for _, group := range groupList.groups {

			// Intersect the set of the current candidate with each member of the groupList.  The result is added to the
			// Pending sets
			set := candidate.set.intersect(group.set)
			if set.len() >= 3 {
				pendingSets = append(pendingSets, set)
			}
		}

		// Add the sets in the pending list to the groupList
		for _, set := range pendingSets {
			groupList.addSet(set)
		}
	}

	// Second iteration - Per groupList element. count the number of candidates that are part of that element.
	// Since every candidate represents a host, if the number of participants == the set size, then there is a full
	// mesh connectivity between the members
	for _, candidate := range groupCandidates {
		for _, cs := range groupList.groups {
			if candidate.set.isSupersetOf(cs.set) && cs.set.containsElement(candidate.me) {
				cs.count++
			}
		}
	}
	return groupList
}

func filterFullMeshGroups(groupList connectivityGroupList) []*connectivitySet {
	ret := make([]*connectivitySet, 0)
	for _, r := range groupList.groups {
		// Add only sets with full mesh connectivity
		if r.set.len() == r.count {
			ret = append(ret, r.set)
		}
	}
	return ret
}

// Create sorted list of connectivity sets.  The sort is by set size (descending)
func createConnectivityGroups(groupCandidates []groupCandidate) []*connectivitySet {
	groupList := createGroupList(groupCandidates)
	ret := filterFullMeshGroups(groupList)

	// Sort by set size descending, which means the largest group first.
	sort.Slice(ret, func(i, j int) bool {
		// If the sizes are equal then compare the contained elements of each set
		return ret[i].id().isLess(ret[j].id())
	})
	return ret
}

/*
 * Create group candidate for a specific host.  The group candidate contains a set with all the hosts it has
 * connectivity to.
 */
func createHostGroupCandidate(hostIndex, numHosts int, cMap connectivityMap) groupCandidate {
	set := NewConnectivitySet(numHosts)
	_ = set.add(hostIndex)
	for index := 0; index != numHosts; index++ {
		if cMap.isConnected(hostIndex, index) {
			_ = set.add(index)
		}
	}
	return groupCandidate{
		set: set,
		me:  hostIndex,
	}
}

type hostQueryFactory interface {
	create(h *models.Host) (hostQuery, error)
}

type hostQuery interface {
	next() strfmt.UUID
}

type l2Query struct {
	parsedCidr         *net.IPNet
	current            int
	connectivityReport models.ConnectivityReport
}

func (l *l2Query) next() strfmt.UUID {
	for l.current != len(l.connectivityReport.RemoteHosts) {
		rh := l.connectivityReport.RemoteHosts[l.current]
		l.current++
		for _, l2 := range rh.L2Connectivity {
			ip := net.ParseIP(l2.RemoteIPAddress)
			if ip != nil && l.parsedCidr.Contains(ip) && l2.Successful {
				return rh.HostID
			}
		}
	}
	return ""
}

type l2QueryFactory struct {
	parsedCidr *net.IPNet
}

func (l *l2QueryFactory) create(h *models.Host) (hostQuery, error) {
	ret := l2Query{
		parsedCidr: l.parsedCidr,
	}
	err := json.Unmarshal([]byte(h.Connectivity), &ret.connectivityReport)
	if err != nil {
		return nil, err
	}
	return &ret, nil
}

func newL2QueryFactory(cidr string) (hostQueryFactory, error) {
	_, parsedCidr, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	return &l2QueryFactory{
		parsedCidr: parsedCidr,
	}, nil
}

type l3Query struct {
	current            int
	connectivityReport models.ConnectivityReport
	nodesAddresses     map[strfmt.UUID]map[string]bool
}

func (l *l3Query) next() strfmt.UUID {
	for l.current != len(l.connectivityReport.RemoteHosts) {
		rh := l.connectivityReport.RemoteHosts[l.current]
		l.current++
		addresses, ok := l.nodesAddresses[rh.HostID]
		if !ok {
			continue
		}
		foundAddresses := make(map[string]bool)
		for _, l3 := range rh.L3Connectivity {
			_, foundAddress := addresses[l3.RemoteIPAddress]
			if foundAddress && l3.Successful {
				foundAddresses[l3.RemoteIPAddress] = true
			}
		}
		if len(addresses) == len(foundAddresses) {
			return rh.HostID
		}
	}
	return ""
}

type l3QueryFactory struct {
	nodesAddresses map[strfmt.UUID]map[string]bool
}

func (l *l3QueryFactory) create(h *models.Host) (hostQuery, error) {
	ret := l3Query{
		nodesAddresses: l.nodesAddresses,
	}
	err := json.Unmarshal([]byte(h.Connectivity), &ret.connectivityReport)
	if err != nil {
		return nil, err
	}
	return &ret, nil
}

func newL3QueryFactory(hosts []*models.Host, family AddressFamily) (hostQueryFactory, error) {
	nodesAddresses := make(map[strfmt.UUID]map[string]bool)
	for _, h := range hosts {
		if h.Inventory == "" {
			continue
		}
		value := make(map[string]bool)
		inventory, err := hostutil.UnmarshalInventory(h.Inventory)
		if err != nil {
			return nil, err
		}
		for _, intf := range inventory.Interfaces {
			var array []string
			switch family {
			case IPv4:
				array = intf.IPV4Addresses
			case IPv6:
				array = intf.IPV6Addresses
			}
			for _, addr := range array {
				ip, _, err := net.ParseCIDR(addr)
				if err != nil {
					return nil, err
				}
				value[ip.String()] = true
			}
		}
		nodesAddresses[*h.ID] = value
	}
	return &l3QueryFactory{
		nodesAddresses: nodesAddresses,
	}, nil
}

type majorityGroupCalculator struct {
	hostQueryFactory hostQueryFactory
}

/*
 * Create connectivity map from host list.  It is the information if a host has connectivity to other host
 */
func (m *majorityGroupCalculator) createConnectivityMap(hosts []*models.Host, idToIndex map[strfmt.UUID]int) (connectivityMap, error) {
	ret := make(connectivityMap)
	for fromIndex, h := range hosts {
		if h.Connectivity == "" {
			continue
		}
		query, err := m.hostQueryFactory.create(h)
		if err != nil {
			return nil, err
		}
		for hid := query.next(); hid != ""; hid = query.next() {
			toIndex, ok := idToIndex[hid]
			if ok {
				ret.add(fromIndex, toIndex, true)
			}
		}
	}
	return ret, nil
}

func (m *majorityGroupCalculator) createMajorityGroup(hosts []*models.Host) ([]strfmt.UUID, error) {
	idToIndex := make(map[strfmt.UUID]int)
	for i, h := range hosts {
		idToIndex[*h.ID] = i
	}
	cMap, err := m.createConnectivityMap(hosts, idToIndex)
	if err != nil {
		return nil, err
	}
	candidates := make([]groupCandidate, 0)
	for hostIndex := range hosts {
		candidate := createHostGroupCandidate(hostIndex, len(hosts), cMap)
		if candidate.set.len() >= 3 {
			candidates = append(candidates, candidate)
		}
	}
	groups := createConnectivityGroups(candidates)
	if len(groups) > 0 {
		return groups[0].toList(hosts), nil
	}
	return make([]strfmt.UUID, 0), nil
}

func calculateMajoryGroup(hosts []*models.Host, factory hostQueryFactory) ([]strfmt.UUID, error) {
	calc := &majorityGroupCalculator{
		hostQueryFactory: factory,
	}
	return calc.createMajorityGroup(hosts)
}

/*
 * Crate majority for a cidr.  A majority group is a the largest group of hosts in a cluster that all of them have full mesh
 * to the other group members.
 * It is done by taking a sorted connectivity group list according to the group size, and from this group take the
 * largest one
 */
func CreateL2MajorityGroup(cidr string, hosts []*models.Host) ([]strfmt.UUID, error) {
	factory, err := newL2QueryFactory(cidr)
	if err != nil {
		return nil, err
	}
	return calculateMajoryGroup(hosts, factory)
}

/*
 * Crate majority for address family.  A majority group is a the largest group of hosts in a cluster that all of them have full mesh
 * to the other group members.
 * It is done by taking a sorted connectivity group list according to the group size, and from this group take the
 * largest one
 */
func CreateL3MajorityGroup(hosts []*models.Host, family AddressFamily) ([]strfmt.UUID, error) {
	if !funk.Contains([]AddressFamily{IPv4, IPv6}, family) {
		return nil, errors.Errorf("Unexpected address family %+v", family)
	}
	factory, err := newL3QueryFactory(hosts, family)
	if err != nil {
		return nil, err
	}
	return calculateMajoryGroup(hosts, factory)
}
