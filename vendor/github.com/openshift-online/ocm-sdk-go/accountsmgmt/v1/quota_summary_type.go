/*
Copyright (c) 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// IMPORTANT: This file has been generated automatically, refrain from modifying it manually as all
// your changes will be lost when the file is generated again.

package v1 // github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1

// QuotaSummary represents the values of the 'quota_summary' type.
//
//
type QuotaSummary struct {
	bitmap_              uint32
	allowed              int
	availabilityZoneType string
	organizationID       string
	reserved             int
	resourceName         string
	resourceType         string
	byoc                 bool
}

// Empty returns true if the object is empty, i.e. no attribute has a value.
func (o *QuotaSummary) Empty() bool {
	return o == nil || o.bitmap_ == 0
}

// BYOC returns the value of the 'BYOC' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) BYOC() bool {
	if o != nil && o.bitmap_&1 != 0 {
		return o.byoc
	}
	return false
}

// GetBYOC returns the value of the 'BYOC' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetBYOC() (value bool, ok bool) {
	ok = o != nil && o.bitmap_&1 != 0
	if ok {
		value = o.byoc
	}
	return
}

// Allowed returns the value of the 'allowed' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) Allowed() int {
	if o != nil && o.bitmap_&2 != 0 {
		return o.allowed
	}
	return 0
}

// GetAllowed returns the value of the 'allowed' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetAllowed() (value int, ok bool) {
	ok = o != nil && o.bitmap_&2 != 0
	if ok {
		value = o.allowed
	}
	return
}

// AvailabilityZoneType returns the value of the 'availability_zone_type' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) AvailabilityZoneType() string {
	if o != nil && o.bitmap_&4 != 0 {
		return o.availabilityZoneType
	}
	return ""
}

// GetAvailabilityZoneType returns the value of the 'availability_zone_type' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetAvailabilityZoneType() (value string, ok bool) {
	ok = o != nil && o.bitmap_&4 != 0
	if ok {
		value = o.availabilityZoneType
	}
	return
}

// OrganizationID returns the value of the 'organization_ID' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) OrganizationID() string {
	if o != nil && o.bitmap_&8 != 0 {
		return o.organizationID
	}
	return ""
}

// GetOrganizationID returns the value of the 'organization_ID' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetOrganizationID() (value string, ok bool) {
	ok = o != nil && o.bitmap_&8 != 0
	if ok {
		value = o.organizationID
	}
	return
}

// Reserved returns the value of the 'reserved' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) Reserved() int {
	if o != nil && o.bitmap_&16 != 0 {
		return o.reserved
	}
	return 0
}

// GetReserved returns the value of the 'reserved' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetReserved() (value int, ok bool) {
	ok = o != nil && o.bitmap_&16 != 0
	if ok {
		value = o.reserved
	}
	return
}

// ResourceName returns the value of the 'resource_name' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) ResourceName() string {
	if o != nil && o.bitmap_&32 != 0 {
		return o.resourceName
	}
	return ""
}

// GetResourceName returns the value of the 'resource_name' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetResourceName() (value string, ok bool) {
	ok = o != nil && o.bitmap_&32 != 0
	if ok {
		value = o.resourceName
	}
	return
}

// ResourceType returns the value of the 'resource_type' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *QuotaSummary) ResourceType() string {
	if o != nil && o.bitmap_&64 != 0 {
		return o.resourceType
	}
	return ""
}

// GetResourceType returns the value of the 'resource_type' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *QuotaSummary) GetResourceType() (value string, ok bool) {
	ok = o != nil && o.bitmap_&64 != 0
	if ok {
		value = o.resourceType
	}
	return
}

// QuotaSummaryListKind is the name of the type used to represent list of objects of
// type 'quota_summary'.
const QuotaSummaryListKind = "QuotaSummaryList"

// QuotaSummaryListLinkKind is the name of the type used to represent links to list
// of objects of type 'quota_summary'.
const QuotaSummaryListLinkKind = "QuotaSummaryListLink"

// QuotaSummaryNilKind is the name of the type used to nil lists of objects of
// type 'quota_summary'.
const QuotaSummaryListNilKind = "QuotaSummaryListNil"

// QuotaSummaryList is a list of values of the 'quota_summary' type.
type QuotaSummaryList struct {
	href  string
	link  bool
	items []*QuotaSummary
}

// Len returns the length of the list.
func (l *QuotaSummaryList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.items)
}

// Empty returns true if the list is empty.
func (l *QuotaSummaryList) Empty() bool {
	return l == nil || len(l.items) == 0
}

// Get returns the item of the list with the given index. If there is no item with
// that index it returns nil.
func (l *QuotaSummaryList) Get(i int) *QuotaSummary {
	if l == nil || i < 0 || i >= len(l.items) {
		return nil
	}
	return l.items[i]
}

// Slice returns an slice containing the items of the list. The returned slice is a
// copy of the one used internally, so it can be modified without affecting the
// internal representation.
//
// If you don't need to modify the returned slice consider using the Each or Range
// functions, as they don't need to allocate a new slice.
func (l *QuotaSummaryList) Slice() []*QuotaSummary {
	var slice []*QuotaSummary
	if l == nil {
		slice = make([]*QuotaSummary, 0)
	} else {
		slice = make([]*QuotaSummary, len(l.items))
		copy(slice, l.items)
	}
	return slice
}

// Each runs the given function for each item of the list, in order. If the function
// returns false the iteration stops, otherwise it continues till all the elements
// of the list have been processed.
func (l *QuotaSummaryList) Each(f func(item *QuotaSummary) bool) {
	if l == nil {
		return
	}
	for _, item := range l.items {
		if !f(item) {
			break
		}
	}
}

// Range runs the given function for each index and item of the list, in order. If
// the function returns false the iteration stops, otherwise it continues till all
// the elements of the list have been processed.
func (l *QuotaSummaryList) Range(f func(index int, item *QuotaSummary) bool) {
	if l == nil {
		return
	}
	for index, item := range l.items {
		if !f(index, item) {
			break
		}
	}
}
