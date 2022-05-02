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

import (
	time "time"
)

// ResourceBuilder contains the data and logic needed to build 'resource' objects.
//
// A Resource which belongs to a cluster, example Cluster Deployment.
type ResourceBuilder struct {
	bitmap_           uint32
	id                string
	href              string
	clusterID         string
	creationTimestamp time.Time
	resources         map[string]string
}

// NewResource creates a new builder of 'resource' objects.
func NewResource() *ResourceBuilder {
	return &ResourceBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *ResourceBuilder) Link(value bool) *ResourceBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *ResourceBuilder) ID(value string) *ResourceBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *ResourceBuilder) HREF(value string) *ResourceBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// ClusterID sets the value of the 'cluster_ID' attribute to the given value.
//
//
func (b *ResourceBuilder) ClusterID(value string) *ResourceBuilder {
	b.clusterID = value
	b.bitmap_ |= 8
	return b
}

// CreationTimestamp sets the value of the 'creation_timestamp' attribute to the given value.
//
//
func (b *ResourceBuilder) CreationTimestamp(value time.Time) *ResourceBuilder {
	b.creationTimestamp = value
	b.bitmap_ |= 16
	return b
}

// Resources sets the value of the 'resources' attribute to the given value.
//
//
func (b *ResourceBuilder) Resources(value map[string]string) *ResourceBuilder {
	b.resources = value
	if value != nil {
		b.bitmap_ |= 32
	} else {
		b.bitmap_ &^= 32
	}
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *ResourceBuilder) Copy(object *Resource) *ResourceBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.clusterID = object.clusterID
	b.creationTimestamp = object.creationTimestamp
	if len(object.resources) > 0 {
		b.resources = map[string]string{}
		for k, v := range object.resources {
			b.resources[k] = v
		}
	} else {
		b.resources = nil
	}
	return b
}

// Build creates a 'resource' object using the configuration stored in the builder.
func (b *ResourceBuilder) Build() (object *Resource, err error) {
	object = new(Resource)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.clusterID = b.clusterID
	object.creationTimestamp = b.creationTimestamp
	if b.resources != nil {
		object.resources = make(map[string]string)
		for k, v := range b.resources {
			object.resources[k] = v
		}
	}
	return
}
