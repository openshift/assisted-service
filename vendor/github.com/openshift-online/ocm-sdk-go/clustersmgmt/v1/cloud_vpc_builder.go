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

// CloudVPCBuilder contains the data and logic needed to build 'cloud_VPC' objects.
//
// Description of a cloud provider virtual private cloud.
type CloudVPCBuilder struct {
	bitmap_ uint32
	name    string
	subnets []string
}

// NewCloudVPC creates a new builder of 'cloud_VPC' objects.
func NewCloudVPC() *CloudVPCBuilder {
	return &CloudVPCBuilder{}
}

// Name sets the value of the 'name' attribute to the given value.
//
//
func (b *CloudVPCBuilder) Name(value string) *CloudVPCBuilder {
	b.name = value
	b.bitmap_ |= 1
	return b
}

// Subnets sets the value of the 'subnets' attribute to the given values.
//
//
func (b *CloudVPCBuilder) Subnets(values ...string) *CloudVPCBuilder {
	b.subnets = make([]string, len(values))
	copy(b.subnets, values)
	b.bitmap_ |= 2
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *CloudVPCBuilder) Copy(object *CloudVPC) *CloudVPCBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.name = object.name
	if object.subnets != nil {
		b.subnets = make([]string, len(object.subnets))
		copy(b.subnets, object.subnets)
	} else {
		b.subnets = nil
	}
	return b
}

// Build creates a 'cloud_VPC' object using the configuration stored in the builder.
func (b *CloudVPCBuilder) Build() (object *CloudVPC, err error) {
	object = new(CloudVPC)
	object.bitmap_ = b.bitmap_
	object.name = b.name
	if b.subnets != nil {
		object.subnets = make([]string, len(b.subnets))
		copy(object.subnets, b.subnets)
	}
	return
}
