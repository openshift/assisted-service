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

package v1 // github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1

// AddOnRequirementBuilder contains the data and logic needed to build 'add_on_requirement' objects.
//
// Representation of an add-on requirement.
type AddOnRequirementBuilder struct {
	bitmap_  uint32
	id       string
	data     map[string]interface{}
	resource string
	enabled  bool
}

// NewAddOnRequirement creates a new builder of 'add_on_requirement' objects.
func NewAddOnRequirement() *AddOnRequirementBuilder {
	return &AddOnRequirementBuilder{}
}

// ID sets the value of the 'ID' attribute to the given value.
//
//
func (b *AddOnRequirementBuilder) ID(value string) *AddOnRequirementBuilder {
	b.id = value
	b.bitmap_ |= 1
	return b
}

// Data sets the value of the 'data' attribute to the given value.
//
//
func (b *AddOnRequirementBuilder) Data(value map[string]interface{}) *AddOnRequirementBuilder {
	b.data = value
	if value != nil {
		b.bitmap_ |= 2
	} else {
		b.bitmap_ &^= 2
	}
	return b
}

// Enabled sets the value of the 'enabled' attribute to the given value.
//
//
func (b *AddOnRequirementBuilder) Enabled(value bool) *AddOnRequirementBuilder {
	b.enabled = value
	b.bitmap_ |= 4
	return b
}

// Resource sets the value of the 'resource' attribute to the given value.
//
//
func (b *AddOnRequirementBuilder) Resource(value string) *AddOnRequirementBuilder {
	b.resource = value
	b.bitmap_ |= 8
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *AddOnRequirementBuilder) Copy(object *AddOnRequirement) *AddOnRequirementBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	if len(object.data) > 0 {
		b.data = map[string]interface{}{}
		for k, v := range object.data {
			b.data[k] = v
		}
	} else {
		b.data = nil
	}
	b.enabled = object.enabled
	b.resource = object.resource
	return b
}

// Build creates a 'add_on_requirement' object using the configuration stored in the builder.
func (b *AddOnRequirementBuilder) Build() (object *AddOnRequirement, err error) {
	object = new(AddOnRequirement)
	object.bitmap_ = b.bitmap_
	object.id = b.id
	if b.data != nil {
		object.data = make(map[string]interface{})
		for k, v := range b.data {
			object.data[k] = v
		}
	}
	object.enabled = b.enabled
	object.resource = b.resource
	return
}
