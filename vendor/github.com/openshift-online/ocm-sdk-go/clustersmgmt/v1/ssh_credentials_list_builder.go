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

// SSHCredentialsListBuilder contains the data and logic needed to build
// 'SSH_credentials' objects.
type SSHCredentialsListBuilder struct {
	items []*SSHCredentialsBuilder
}

// NewSSHCredentialsList creates a new builder of 'SSH_credentials' objects.
func NewSSHCredentialsList() *SSHCredentialsListBuilder {
	return new(SSHCredentialsListBuilder)
}

// Items sets the items of the list.
func (b *SSHCredentialsListBuilder) Items(values ...*SSHCredentialsBuilder) *SSHCredentialsListBuilder {
	b.items = make([]*SSHCredentialsBuilder, len(values))
	copy(b.items, values)
	return b
}

// Copy copies the items of the given list into this builder, discarding any previous items.
func (b *SSHCredentialsListBuilder) Copy(list *SSHCredentialsList) *SSHCredentialsListBuilder {
	if list == nil || list.items == nil {
		b.items = nil
	} else {
		b.items = make([]*SSHCredentialsBuilder, len(list.items))
		for i, v := range list.items {
			b.items[i] = NewSSHCredentials().Copy(v)
		}
	}
	return b
}

// Build creates a list of 'SSH_credentials' objects using the
// configuration stored in the builder.
func (b *SSHCredentialsListBuilder) Build() (list *SSHCredentialsList, err error) {
	items := make([]*SSHCredentials, len(b.items))
	for i, item := range b.items {
		items[i], err = item.Build()
		if err != nil {
			return
		}
	}
	list = new(SSHCredentialsList)
	list.items = items
	return
}
