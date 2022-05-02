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

// GCPFlavourBuilder contains the data and logic needed to build 'GCP_flavour' objects.
//
// Specification for different classes of nodes inside a flavour.
type GCPFlavourBuilder struct {
	bitmap_             uint32
	computeInstanceType string
	infraInstanceType   string
	masterInstanceType  string
}

// NewGCPFlavour creates a new builder of 'GCP_flavour' objects.
func NewGCPFlavour() *GCPFlavourBuilder {
	return &GCPFlavourBuilder{}
}

// ComputeInstanceType sets the value of the 'compute_instance_type' attribute to the given value.
//
//
func (b *GCPFlavourBuilder) ComputeInstanceType(value string) *GCPFlavourBuilder {
	b.computeInstanceType = value
	b.bitmap_ |= 1
	return b
}

// InfraInstanceType sets the value of the 'infra_instance_type' attribute to the given value.
//
//
func (b *GCPFlavourBuilder) InfraInstanceType(value string) *GCPFlavourBuilder {
	b.infraInstanceType = value
	b.bitmap_ |= 2
	return b
}

// MasterInstanceType sets the value of the 'master_instance_type' attribute to the given value.
//
//
func (b *GCPFlavourBuilder) MasterInstanceType(value string) *GCPFlavourBuilder {
	b.masterInstanceType = value
	b.bitmap_ |= 4
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *GCPFlavourBuilder) Copy(object *GCPFlavour) *GCPFlavourBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.computeInstanceType = object.computeInstanceType
	b.infraInstanceType = object.infraInstanceType
	b.masterInstanceType = object.masterInstanceType
	return b
}

// Build creates a 'GCP_flavour' object using the configuration stored in the builder.
func (b *GCPFlavourBuilder) Build() (object *GCPFlavour, err error) {
	object = new(GCPFlavour)
	object.bitmap_ = b.bitmap_
	object.computeInstanceType = b.computeInstanceType
	object.infraInstanceType = b.infraInstanceType
	object.masterInstanceType = b.masterInstanceType
	return
}
