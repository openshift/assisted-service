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

// ServerConfigBuilder contains the data and logic needed to build 'server_config' objects.
//
// Representation of a server config
type ServerConfigBuilder struct {
	bitmap_ uint32
	id      string
	href    string
	server  string
}

// NewServerConfig creates a new builder of 'server_config' objects.
func NewServerConfig() *ServerConfigBuilder {
	return &ServerConfigBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *ServerConfigBuilder) Link(value bool) *ServerConfigBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *ServerConfigBuilder) ID(value string) *ServerConfigBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *ServerConfigBuilder) HREF(value string) *ServerConfigBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// Server sets the value of the 'server' attribute to the given value.
//
//
func (b *ServerConfigBuilder) Server(value string) *ServerConfigBuilder {
	b.server = value
	b.bitmap_ |= 8
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *ServerConfigBuilder) Copy(object *ServerConfig) *ServerConfigBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.server = object.server
	return b
}

// Build creates a 'server_config' object using the configuration stored in the builder.
func (b *ServerConfigBuilder) Build() (object *ServerConfig, err error) {
	object = new(ServerConfig)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.server = b.server
	return
}
