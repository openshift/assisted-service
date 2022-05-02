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

// SKUKind is the name of the type used to represent objects
// of type 'SKU'.
const SKUKind = "SKU"

// SKULinkKind is the name of the type used to represent links
// to objects of type 'SKU'.
const SKULinkKind = "SKULink"

// SKUNilKind is the name of the type used to nil references
// to objects of type 'SKU'.
const SKUNilKind = "SKUNil"

// SKU represents the values of the 'SKU' type.
//
// Identifies computing resources
type SKU struct {
	bitmap_              uint32
	id                   string
	href                 string
	availabilityZoneType string
	resourceName         string
	resourceType         string
	resources            []*Resource
	byoc                 bool
}

// Kind returns the name of the type of the object.
func (o *SKU) Kind() string {
	if o == nil {
		return SKUNilKind
	}
	if o.bitmap_&1 != 0 {
		return SKULinkKind
	}
	return SKUKind
}

// Link returns true iif this is a link.
func (o *SKU) Link() bool {
	return o != nil && o.bitmap_&1 != 0
}

// ID returns the identifier of the object.
func (o *SKU) ID() string {
	if o != nil && o.bitmap_&2 != 0 {
		return o.id
	}
	return ""
}

// GetID returns the identifier of the object and a flag indicating if the
// identifier has a value.
func (o *SKU) GetID() (value string, ok bool) {
	ok = o != nil && o.bitmap_&2 != 0
	if ok {
		value = o.id
	}
	return
}

// HREF returns the link to the object.
func (o *SKU) HREF() string {
	if o != nil && o.bitmap_&4 != 0 {
		return o.href
	}
	return ""
}

// GetHREF returns the link of the object and a flag indicating if the
// link has a value.
func (o *SKU) GetHREF() (value string, ok bool) {
	ok = o != nil && o.bitmap_&4 != 0
	if ok {
		value = o.href
	}
	return
}

// Empty returns true if the object is empty, i.e. no attribute has a value.
func (o *SKU) Empty() bool {
	return o == nil || o.bitmap_&^1 == 0
}

// BYOC returns the value of the 'BYOC' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *SKU) BYOC() bool {
	if o != nil && o.bitmap_&8 != 0 {
		return o.byoc
	}
	return false
}

// GetBYOC returns the value of the 'BYOC' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *SKU) GetBYOC() (value bool, ok bool) {
	ok = o != nil && o.bitmap_&8 != 0
	if ok {
		value = o.byoc
	}
	return
}

// AvailabilityZoneType returns the value of the 'availability_zone_type' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *SKU) AvailabilityZoneType() string {
	if o != nil && o.bitmap_&16 != 0 {
		return o.availabilityZoneType
	}
	return ""
}

// GetAvailabilityZoneType returns the value of the 'availability_zone_type' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *SKU) GetAvailabilityZoneType() (value string, ok bool) {
	ok = o != nil && o.bitmap_&16 != 0
	if ok {
		value = o.availabilityZoneType
	}
	return
}

// ResourceName returns the value of the 'resource_name' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
// platform-specific name, such as "M5.2Xlarge" for a type of EC2 node
func (o *SKU) ResourceName() string {
	if o != nil && o.bitmap_&32 != 0 {
		return o.resourceName
	}
	return ""
}

// GetResourceName returns the value of the 'resource_name' attribute and
// a flag indicating if the attribute has a value.
//
// platform-specific name, such as "M5.2Xlarge" for a type of EC2 node
func (o *SKU) GetResourceName() (value string, ok bool) {
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
func (o *SKU) ResourceType() string {
	if o != nil && o.bitmap_&64 != 0 {
		return o.resourceType
	}
	return ""
}

// GetResourceType returns the value of the 'resource_type' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *SKU) GetResourceType() (value string, ok bool) {
	ok = o != nil && o.bitmap_&64 != 0
	if ok {
		value = o.resourceType
	}
	return
}

// Resources returns the value of the 'resources' attribute, or
// the zero value of the type if the attribute doesn't have a value.
//
//
func (o *SKU) Resources() []*Resource {
	if o != nil && o.bitmap_&128 != 0 {
		return o.resources
	}
	return nil
}

// GetResources returns the value of the 'resources' attribute and
// a flag indicating if the attribute has a value.
//
//
func (o *SKU) GetResources() (value []*Resource, ok bool) {
	ok = o != nil && o.bitmap_&128 != 0
	if ok {
		value = o.resources
	}
	return
}

// SKUListKind is the name of the type used to represent list of objects of
// type 'SKU'.
const SKUListKind = "SKUList"

// SKUListLinkKind is the name of the type used to represent links to list
// of objects of type 'SKU'.
const SKUListLinkKind = "SKUListLink"

// SKUNilKind is the name of the type used to nil lists of objects of
// type 'SKU'.
const SKUListNilKind = "SKUListNil"

// SKUList is a list of values of the 'SKU' type.
type SKUList struct {
	href  string
	link  bool
	items []*SKU
}

// Kind returns the name of the type of the object.
func (l *SKUList) Kind() string {
	if l == nil {
		return SKUListNilKind
	}
	if l.link {
		return SKUListLinkKind
	}
	return SKUListKind
}

// Link returns true iif this is a link.
func (l *SKUList) Link() bool {
	return l != nil && l.link
}

// HREF returns the link to the list.
func (l *SKUList) HREF() string {
	if l != nil {
		return l.href
	}
	return ""
}

// GetHREF returns the link of the list and a flag indicating if the
// link has a value.
func (l *SKUList) GetHREF() (value string, ok bool) {
	ok = l != nil && l.href != ""
	if ok {
		value = l.href
	}
	return
}

// Len returns the length of the list.
func (l *SKUList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.items)
}

// Empty returns true if the list is empty.
func (l *SKUList) Empty() bool {
	return l == nil || len(l.items) == 0
}

// Get returns the item of the list with the given index. If there is no item with
// that index it returns nil.
func (l *SKUList) Get(i int) *SKU {
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
func (l *SKUList) Slice() []*SKU {
	var slice []*SKU
	if l == nil {
		slice = make([]*SKU, 0)
	} else {
		slice = make([]*SKU, len(l.items))
		copy(slice, l.items)
	}
	return slice
}

// Each runs the given function for each item of the list, in order. If the function
// returns false the iteration stops, otherwise it continues till all the elements
// of the list have been processed.
func (l *SKUList) Each(f func(item *SKU) bool) {
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
func (l *SKUList) Range(f func(index int, item *SKU) bool) {
	if l == nil {
		return
	}
	for index, item := range l.items {
		if !f(index, item) {
			break
		}
	}
}
