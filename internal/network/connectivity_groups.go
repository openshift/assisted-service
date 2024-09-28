package network

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/go-openapi/strfmt"
	"github.com/golang-collections/go-datastructures/bitarray"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"golang.org/x/sys/unix"
)

type AddressFamily int

const (
	IPv4 AddressFamily = unix.AF_INET
	IPv6 AddressFamily = unix.AF_INET6
)

type Connectivity struct {
	MajorityGroups       map[string][]strfmt.UUID `json:"majority_groups"`
	L3ConnectedAddresses map[strfmt.UUID][]string `json:"l3_connected_addresses"`
}

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

func (c *connectivitySet) union(other *connectivitySet) *connectivitySet {
	return &connectivitySet{
		array: c.array.Or(other.array),
	}
}

func (c *connectivitySet) containsElement(id int) bool {
	b, err := c.array.GetBit(uint64(id))
	return err == nil && b
}

func (c *connectivitySet) id() groupId {
	if c.groupId == nil {
		c.groupId = c.array.ToNums()
	}
	return c.groupId
}

func (c *connectivitySet) equals(other *connectivitySet) bool {
	return c.array.Equals(other.array)
}

func (c *connectivitySet) toList(hosts []*models.Host) []strfmt.UUID {
	ret := make([]strfmt.UUID, 0)
	for _, k := range c.id() {
		ret = append(ret, *hosts[k].ID)
	}
	return ret
}

func (c *connectivitySet) size() int {
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
	set                        *connectivitySet
	participatingHosts         *connectivitySet
	participatingPrimaryGroups *connectivitySet
}

func (c *connectivityGroup) id() groupId {
	return c.set.id()
}

func (c *connectivityGroup) size() int {
	return c.set.size()
}

// Merge participants between groups
func (c *connectivityGroup) mergeParticipants(cg *connectivityGroup) {

	// The participating hosts are the hosts from both groups that all appear in the connectivity group set
	c.participatingHosts = c.set.intersect(c.participatingHosts.union(cg.participatingHosts))

	if c.participatingPrimaryGroups != nil && cg.participatingPrimaryGroups != nil {
		// Also merge the original groups
		c.participatingPrimaryGroups = c.participatingPrimaryGroups.union(cg.participatingPrimaryGroups)
	}
}

// When intersecting groups, the sets are intersected, and the participating hosts and groups are merged.
func (c *connectivityGroup) intersect(cg *connectivityGroup) *connectivityGroup {
	ret := &connectivityGroup{
		set:                        c.set.intersect(cg.set),
		participatingHosts:         c.participatingHosts,
		participatingPrimaryGroups: c.participatingPrimaryGroups,
	}
	ret.mergeParticipants(cg)
	return ret
}

func (c *connectivityGroup) setPrimaryGroup(primaryGroupsNum, groupIndex int) {
	if c.participatingPrimaryGroups == nil {
		c.participatingPrimaryGroups = NewConnectivitySet(primaryGroupsNum)
	}
	_ = c.participatingPrimaryGroups.add(groupIndex)
}

func (c *connectivityGroup) numParticipatingHosts() int {
	return c.participatingHosts.size()
}

func (c *connectivityGroup) isFullMesh() bool {
	return c.set.size() == c.participatingHosts.size()
}

func (c *connectivityGroup) equivalent(other *connectivityGroup) bool {
	return c.set.equals(other.set)
}

// Unique list of connectivity group elements.  The uniqueness is by the set elements
type connectivityGroupList struct {
	groupsBySize map[int][]*connectivityGroup
}

func newConnectivityGroupList() *connectivityGroupList {
	return &connectivityGroupList{
		groupsBySize: make(map[int][]*connectivityGroup),
	}
}

func (c *connectivityGroupList) findGroup(cg *connectivityGroup) *connectivityGroup {
	groupsForSize, exists := c.groupsBySize[cg.size()]
	if !exists {
		return nil
	}
	for _, g := range groupsForSize {
		if g.equivalent(cg) {
			return g
		}
	}
	return nil
}

func (c *connectivityGroupList) addGroup(cg *connectivityGroup) {
	size := cg.size()
	c.groupsBySize[size] = append(c.groupsBySize[size], cg)
}

func (m *majorityGroupCalculator) toConnectivityGroup(candidate *groupCandidate) *connectivityGroup {
	ret := &connectivityGroup{
		set:                candidate.set,
		participatingHosts: NewConnectivitySet(m.numHosts),
	}
	_ = ret.participatingHosts.add(candidate.me)
	return ret
}

func (c *connectivityGroupList) addOrMergeGroup(cg *connectivityGroup) {
	foundGroup := c.findGroup(cg)
	if foundGroup != nil {
		foundGroup.mergeParticipants(cg)
	} else {
		c.addGroup(cg)
	}
}

func (c *connectivityGroupList) largestGroup() *connectivityGroup {
	size := c.biggestGroupSize()
	if size < 3 {
		return nil
	}
	groupsBySize := c.groupsBySize[size]
	var ret *connectivityGroup
	for _, cg := range groupsBySize {
		if ret == nil || ret.numParticipatingHosts() < cg.numParticipatingHosts() ||
			ret.numParticipatingHosts() == cg.numParticipatingHosts() &&
				cg.id().isLess(ret.id()) {
			ret = cg
		}
	}
	return ret
}

func (c *connectivityGroupList) biggestGroupSize() int {
	ret := 0
	for key := range c.groupsBySize {
		ret = max(ret, key)
	}
	return ret
}

func (c *connectivityGroupList) deleteGroupsBySize(size int) {
	delete(c.groupsBySize, size)
}

func (c *connectivityGroupList) toList() (ret []*connectivityGroup) {
	for _, l := range c.groupsBySize {
		ret = append(ret, l...)
	}
	return ret
}

func (c *connectivityGroupList) setAndMergePrimaryGroups() []*connectivityGroup {
	primaryGroups := c.toList()
	for i, cg := range primaryGroups {
		cg.setPrimaryGroup(len(primaryGroups), i)
	}
	return primaryGroups
}

// Group candidate is the connectivity view of a single host (me)
type groupCandidate struct {
	set *connectivitySet
	me  int
}

func (m *majorityGroupCalculator) createFullMeshGroup(groupCandidates []*groupCandidate) *connectivityGroup {
	groupList := newConnectivityGroupList()

	// First iteration - gather the original groups
	for _, candidate := range groupCandidates {

		// Add the set of the current candidate to the pending list
		groupList.addOrMergeGroup(m.toConnectivityGroup(candidate))
	}

	// The primary groups are used for intersection with group candidate which is not full mesh
	primaryGroups := groupList.setAndMergePrimaryGroups()
	for groupCandidate := groupList.largestGroup(); groupCandidate != nil; groupCandidate = groupList.largestGroup() {
		if groupCandidate.isFullMesh() {
			return groupCandidate
		}

		currentSize := groupCandidate.size()

		// Remove all groups with the current size, since we are now looking for smaller size groups
		groupList.deleteGroupsBySize(currentSize)

		// We now want to intersect with groups not smaller than the biggest group(s) still in the group list
		biggestGroupSize := groupList.biggestGroupSize()
		for i := range primaryGroups {

			// Do not intersect with original groups that were already intersected with this group
			if !groupCandidate.participatingPrimaryGroups.containsElement(i) {
				pg := primaryGroups[i]

				// Skip small groups. We are not interested in them now
				if pg.size() < biggestGroupSize {
					continue
				}
				intersectedGroup := groupCandidate.intersect(pg)

				// Groups have to be smaller than the existing size since it was already checked,
				// and greater than 3 (minimal number of hosts for a cluster).
				if intersectedGroup.size() >= 3 && intersectedGroup.size() < currentSize {
					groupList.addOrMergeGroup(intersectedGroup)

					// If the intersected group is larger than the biggest group size we already have, use this one.
					biggestGroupSize = max(intersectedGroup.size(), biggestGroupSize)
				}
			}
		}
	}
	return nil
}

/*
 * Create group candidate for a specific host.  The group candidate contains a set with all the hosts it has
 * connectivity to.
 */
func (m *majorityGroupCalculator) createHostGroupCandidate(hostIndex int, cMap connectivityMap) *groupCandidate {
	set := NewConnectivitySet(m.numHosts)
	_ = set.add(hostIndex)
	for index := 0; index != m.numHosts; index++ {
		if cMap.isConnected(hostIndex, index) {
			_ = set.add(index)
		}
	}
	return &groupCandidate{
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

func (l *l2Query) isIPv6() bool {
	_, bits := l.parsedCidr.Mask.Size()
	return bits == net.IPv6len*8
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
		if l.isIPv6() {
			// nmap uses NDP (Network Discovery Protocol) for IPv6 L2 discovery.  There are cases that NDP responses do not
			// arrive and parsed by nmap, but still there is connectivity between the hosts.  In this case we fallback to L3
			// check and verify that the remote address is in the required subnet.
			for _, l3 := range rh.L3Connectivity {
				ip := net.ParseIP(l3.RemoteIPAddress)
				if ip != nil && l.parsedCidr.Contains(ip) && l3.Successful {
					return rh.HostID
				}
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
		if len(foundAddresses) > 0 && len(addresses) == len(foundAddresses) {
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
		inventory, err := common.UnmarshalInventory(h.Inventory)
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
	numHosts         int
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
	candidates := make([]*groupCandidate, 0)
	for hostIndex := range hosts {
		candidate := m.createHostGroupCandidate(hostIndex, cMap)
		if candidate.set.size() >= 3 {
			candidates = append(candidates, candidate)
		}
	}
	group := m.createFullMeshGroup(candidates)
	if group != nil {
		return group.set.toList(hosts), nil
	}
	return make([]strfmt.UUID, 0), nil
}

func calculateMajorityGroup(hosts []*models.Host, factory hostQueryFactory) ([]strfmt.UUID, error) {
	calc := &majorityGroupCalculator{
		hostQueryFactory: factory,
		numHosts:         len(hosts),
	}
	return calc.createMajorityGroup(hosts)
}

/*
 * Create majority for a cidr.  A majority group is a the largest group of hosts in a cluster that all of them have full mesh
 * to the other group members.
 * It is done by taking a sorted connectivity group list according to the group size, and from this group take the
 * largest one
 */
func CreateL2MajorityGroup(cidr string, hosts []*models.Host) ([]strfmt.UUID, error) {
	factory, err := newL2QueryFactory(cidr)
	if err != nil {
		return nil, err
	}
	return calculateMajorityGroup(hosts, factory)
}

/*
 * Create majority for address family.  A majority group is a the largest group of hosts in a cluster that all of them have full mesh
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
	return calculateMajorityGroup(hosts, factory)
}

func GatherL3ConnectedAddresses(hosts []*models.Host) (map[strfmt.UUID][]string, error) {
	counts := make(map[strfmt.UUID]map[string]int)
	for _, host := range hosts {
		if host.Connectivity == "" {
			continue
		}
		var report models.ConnectivityReport
		if err := json.Unmarshal([]byte(host.Connectivity), &report); err != nil {
			return nil, err
		}
		for _, rh := range report.RemoteHosts {
			if rh.HostID == *host.ID {
				continue
			}
			// There are cases (especially in previous versions of connectivity check) that remote IP address appears
			// more than once in connectivity results. The map is needed in order to verify that a remote ip address
			// is not counted more than once for host-pair.
			hostResults := make(map[string]bool)
			for _, cn := range rh.L3Connectivity {
				if cn.Successful {
					hostResults[cn.RemoteIPAddress] = true
				}
			}
			if len(hostResults) > 0 {
				m, ok := counts[rh.HostID]
				if !ok {
					m = make(map[string]int)
					counts[rh.HostID] = m
				}
				for remoteIpAddress := range hostResults {
					m[remoteIpAddress] = m[remoteIpAddress] + 1
				}
			}
		}
	}
	ret := make(map[strfmt.UUID][]string)
	for hid, hcounts := range counts {
		for addr, count := range hcounts {
			if count == len(hosts)-1 {
				ret[hid] = append(ret[hid], addr)
			}
		}
	}
	return ret, nil
}
