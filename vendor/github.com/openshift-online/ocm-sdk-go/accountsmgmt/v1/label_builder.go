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

import (
	time "time"
)

// LabelBuilder contains the data and logic needed to build 'label' objects.
//
//
type LabelBuilder struct {
	bitmap_   uint32
	id        string
	href      string
	createdAt time.Time
	key       string
	updatedAt time.Time
	value     string
	internal  bool
}

// NewLabel creates a new builder of 'label' objects.
func NewLabel() *LabelBuilder {
	return &LabelBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *LabelBuilder) Link(value bool) *LabelBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *LabelBuilder) ID(value string) *LabelBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *LabelBuilder) HREF(value string) *LabelBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// CreatedAt sets the value of the 'created_at' attribute to the given value.
//
//
func (b *LabelBuilder) CreatedAt(value time.Time) *LabelBuilder {
	b.createdAt = value
	b.bitmap_ |= 8
	return b
}

// Internal sets the value of the 'internal' attribute to the given value.
//
//
func (b *LabelBuilder) Internal(value bool) *LabelBuilder {
	b.internal = value
	b.bitmap_ |= 16
	return b
}

// Key sets the value of the 'key' attribute to the given value.
//
//
func (b *LabelBuilder) Key(value string) *LabelBuilder {
	b.key = value
	b.bitmap_ |= 32
	return b
}

// UpdatedAt sets the value of the 'updated_at' attribute to the given value.
//
//
func (b *LabelBuilder) UpdatedAt(value time.Time) *LabelBuilder {
	b.updatedAt = value
	b.bitmap_ |= 64
	return b
}

// Value sets the value of the 'value' attribute to the given value.
//
//
func (b *LabelBuilder) Value(value string) *LabelBuilder {
	b.value = value
	b.bitmap_ |= 128
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *LabelBuilder) Copy(object *Label) *LabelBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.createdAt = object.createdAt
	b.internal = object.internal
	b.key = object.key
	b.updatedAt = object.updatedAt
	b.value = object.value
	return b
}

// Build creates a 'label' object using the configuration stored in the builder.
func (b *LabelBuilder) Build() (object *Label, err error) {
	object = new(Label)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.createdAt = b.createdAt
	object.internal = b.internal
	object.key = b.key
	object.updatedAt = b.updatedAt
	object.value = b.value
	return
}
