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

// SSHCredentialsBuilder contains the data and logic needed to build 'SSH_credentials' objects.
//
// SSH key pair of a cluster.
type SSHCredentialsBuilder struct {
	bitmap_    uint32
	privateKey string
	publicKey  string
}

// NewSSHCredentials creates a new builder of 'SSH_credentials' objects.
func NewSSHCredentials() *SSHCredentialsBuilder {
	return &SSHCredentialsBuilder{}
}

// PrivateKey sets the value of the 'private_key' attribute to the given value.
//
//
func (b *SSHCredentialsBuilder) PrivateKey(value string) *SSHCredentialsBuilder {
	b.privateKey = value
	b.bitmap_ |= 1
	return b
}

// PublicKey sets the value of the 'public_key' attribute to the given value.
//
//
func (b *SSHCredentialsBuilder) PublicKey(value string) *SSHCredentialsBuilder {
	b.publicKey = value
	b.bitmap_ |= 2
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *SSHCredentialsBuilder) Copy(object *SSHCredentials) *SSHCredentialsBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.privateKey = object.privateKey
	b.publicKey = object.publicKey
	return b
}

// Build creates a 'SSH_credentials' object using the configuration stored in the builder.
func (b *SSHCredentialsBuilder) Build() (object *SSHCredentials, err error) {
	object = new(SSHCredentials)
	object.bitmap_ = b.bitmap_
	object.privateKey = b.privateKey
	object.publicKey = b.publicKey
	return
}
