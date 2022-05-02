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

// CloudProviderBuilder contains the data and logic needed to build 'cloud_provider' objects.
//
// Cloud provider.
type CloudProviderBuilder struct {
	bitmap_     uint32
	id          string
	href        string
	displayName string
	name        string
}

// NewCloudProvider creates a new builder of 'cloud_provider' objects.
func NewCloudProvider() *CloudProviderBuilder {
	return &CloudProviderBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *CloudProviderBuilder) Link(value bool) *CloudProviderBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *CloudProviderBuilder) ID(value string) *CloudProviderBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *CloudProviderBuilder) HREF(value string) *CloudProviderBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// DisplayName sets the value of the 'display_name' attribute to the given value.
//
//
func (b *CloudProviderBuilder) DisplayName(value string) *CloudProviderBuilder {
	b.displayName = value
	b.bitmap_ |= 8
	return b
}

// Name sets the value of the 'name' attribute to the given value.
//
//
func (b *CloudProviderBuilder) Name(value string) *CloudProviderBuilder {
	b.name = value
	b.bitmap_ |= 16
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *CloudProviderBuilder) Copy(object *CloudProvider) *CloudProviderBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.displayName = object.displayName
	b.name = object.name
	return b
}

// Build creates a 'cloud_provider' object using the configuration stored in the builder.
func (b *CloudProviderBuilder) Build() (object *CloudProvider, err error) {
	object = new(CloudProvider)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.displayName = b.displayName
	object.name = b.name
	return
}
