package network

import (
	"encoding/json"
	"net"
	"sort"

	"github.com/go-openapi/strfmt"
	"github.com/golang-collections/go-datastructures/bitarray"
	"github.com/openshift/assisted-service/models"
)

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

/*
 * Create connectivity map from host list.  It is the information if a host has connectivity to other host on specific
 * CIDR (network)
 */
func createMachineCidrConnectivityMap(cidr string, hosts []*models.Host, idToIndex map[strfmt.UUID]int) (connectivityMap, error) {
	ret := make(connectivityMap)
	_, parsedCidr, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	for fromIndex, h := range hosts {
		if h.Connectivity == "" {
			continue
		}
		var connectivityReport models.ConnectivityReport
		err = json.Unmarshal([]byte(h.Connectivity), &connectivityReport)
		if err != nil {
			return nil, err
		}
		for _, r := range connectivityReport.RemoteHosts {
			for _, l2 := range r.L2Connectivity {
				ip := net.ParseIP(l2.RemoteIPAddress)
				if ip != nil && parsedCidr.Contains(ip) && l2.Successful {
					toIndex, ok := idToIndex[r.HostID]
					if ok {
						ret.add(fromIndex, toIndex, true)
					}
					break
				}
			}
		}
	}
	return ret, nil
}

/*
 * Crate majority for a cidr.  A majority group is a the largest group of hosts in a cluster that all of them have full mesh
 * to the other group members.
 * It is done by taking a sorted connectivity group list according to the group size, and from this group take the
 * largest one
 */
func CreateMajorityGroup(cidr string, hosts []*models.Host) ([]strfmt.UUID, error) {
	idToIndex := make(map[strfmt.UUID]int)
	for i, h := range hosts {
		idToIndex[*h.ID] = i
	}
	cMap, err := createMachineCidrConnectivityMap(cidr, hosts, idToIndex)
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
