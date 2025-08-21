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

package v1alpha1 // github.com/openshift-online/ocm-sdk-go/arohcp/v1alpha1

// AzureNodePoolEncryptionAtHostBuilder contains the data and logic needed to build 'azure_node_pool_encryption_at_host' objects.
//
// AzureNodePoolEncryptionAtHost defines the encryption setting for Encryption At Host.
// If not specified, Encryption at Host is not enabled.
type AzureNodePoolEncryptionAtHostBuilder struct {
	bitmap_ uint32
	state   string
}

// NewAzureNodePoolEncryptionAtHost creates a new builder of 'azure_node_pool_encryption_at_host' objects.
func NewAzureNodePoolEncryptionAtHost() *AzureNodePoolEncryptionAtHostBuilder {
	return &AzureNodePoolEncryptionAtHostBuilder{}
}

// Empty returns true if the builder is empty, i.e. no attribute has a value.
func (b *AzureNodePoolEncryptionAtHostBuilder) Empty() bool {
	return b == nil || b.bitmap_ == 0
}

// State sets the value of the 'state' attribute to the given value.
func (b *AzureNodePoolEncryptionAtHostBuilder) State(value string) *AzureNodePoolEncryptionAtHostBuilder {
	b.state = value
	b.bitmap_ |= 1
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *AzureNodePoolEncryptionAtHostBuilder) Copy(object *AzureNodePoolEncryptionAtHost) *AzureNodePoolEncryptionAtHostBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.state = object.state
	return b
}

// Build creates a 'azure_node_pool_encryption_at_host' object using the configuration stored in the builder.
func (b *AzureNodePoolEncryptionAtHostBuilder) Build() (object *AzureNodePoolEncryptionAtHost, err error) {
	object = new(AzureNodePoolEncryptionAtHost)
	object.bitmap_ = b.bitmap_
	object.state = b.state
	return
}
