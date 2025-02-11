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

// QuotaCostBuilder contains the data and logic needed to build 'quota_cost' objects.
//
//
type QuotaCostBuilder struct {
	bitmap_          uint32
	allowed          int
	consumed         int
	organizationID   string
	quotaID          string
	relatedResources []*RelatedResourceBuilder
}

// NewQuotaCost creates a new builder of 'quota_cost' objects.
func NewQuotaCost() *QuotaCostBuilder {
	return &QuotaCostBuilder{}
}

// Allowed sets the value of the 'allowed' attribute to the given value.
//
//
func (b *QuotaCostBuilder) Allowed(value int) *QuotaCostBuilder {
	b.allowed = value
	b.bitmap_ |= 1
	return b
}

// Consumed sets the value of the 'consumed' attribute to the given value.
//
//
func (b *QuotaCostBuilder) Consumed(value int) *QuotaCostBuilder {
	b.consumed = value
	b.bitmap_ |= 2
	return b
}

// OrganizationID sets the value of the 'organization_ID' attribute to the given value.
//
//
func (b *QuotaCostBuilder) OrganizationID(value string) *QuotaCostBuilder {
	b.organizationID = value
	b.bitmap_ |= 4
	return b
}

// QuotaID sets the value of the 'quota_ID' attribute to the given value.
//
//
func (b *QuotaCostBuilder) QuotaID(value string) *QuotaCostBuilder {
	b.quotaID = value
	b.bitmap_ |= 8
	return b
}

// RelatedResources sets the value of the 'related_resources' attribute to the given values.
//
//
func (b *QuotaCostBuilder) RelatedResources(values ...*RelatedResourceBuilder) *QuotaCostBuilder {
	b.relatedResources = make([]*RelatedResourceBuilder, len(values))
	copy(b.relatedResources, values)
	b.bitmap_ |= 16
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *QuotaCostBuilder) Copy(object *QuotaCost) *QuotaCostBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.allowed = object.allowed
	b.consumed = object.consumed
	b.organizationID = object.organizationID
	b.quotaID = object.quotaID
	if object.relatedResources != nil {
		b.relatedResources = make([]*RelatedResourceBuilder, len(object.relatedResources))
		for i, v := range object.relatedResources {
			b.relatedResources[i] = NewRelatedResource().Copy(v)
		}
	} else {
		b.relatedResources = nil
	}
	return b
}

// Build creates a 'quota_cost' object using the configuration stored in the builder.
func (b *QuotaCostBuilder) Build() (object *QuotaCost, err error) {
	object = new(QuotaCost)
	object.bitmap_ = b.bitmap_
	object.allowed = b.allowed
	object.consumed = b.consumed
	object.organizationID = b.organizationID
	object.quotaID = b.quotaID
	if b.relatedResources != nil {
		object.relatedResources = make([]*RelatedResource, len(b.relatedResources))
		for i, v := range b.relatedResources {
			object.relatedResources[i], err = v.Build()
			if err != nil {
				return
			}
		}
	}
	return
}
