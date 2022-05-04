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

package v1 // github.com/openshift-online/ocm-sdk-go/authorizations/v1

// ResourceReviewRequestBuilder contains the data and logic needed to build 'resource_review_request' objects.
//
// Request to perform a resource access review.
type ResourceReviewRequestBuilder struct {
	bitmap_           uint32
	accountUsername   string
	action            string
	resourceType      string
	reduceClusterList bool
}

// NewResourceReviewRequest creates a new builder of 'resource_review_request' objects.
func NewResourceReviewRequest() *ResourceReviewRequestBuilder {
	return &ResourceReviewRequestBuilder{}
}

// AccountUsername sets the value of the 'account_username' attribute to the given value.
//
//
func (b *ResourceReviewRequestBuilder) AccountUsername(value string) *ResourceReviewRequestBuilder {
	b.accountUsername = value
	b.bitmap_ |= 1
	return b
}

// Action sets the value of the 'action' attribute to the given value.
//
//
func (b *ResourceReviewRequestBuilder) Action(value string) *ResourceReviewRequestBuilder {
	b.action = value
	b.bitmap_ |= 2
	return b
}

// ReduceClusterList sets the value of the 'reduce_cluster_list' attribute to the given value.
//
//
func (b *ResourceReviewRequestBuilder) ReduceClusterList(value bool) *ResourceReviewRequestBuilder {
	b.reduceClusterList = value
	b.bitmap_ |= 4
	return b
}

// ResourceType sets the value of the 'resource_type' attribute to the given value.
//
//
func (b *ResourceReviewRequestBuilder) ResourceType(value string) *ResourceReviewRequestBuilder {
	b.resourceType = value
	b.bitmap_ |= 8
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *ResourceReviewRequestBuilder) Copy(object *ResourceReviewRequest) *ResourceReviewRequestBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.accountUsername = object.accountUsername
	b.action = object.action
	b.reduceClusterList = object.reduceClusterList
	b.resourceType = object.resourceType
	return b
}

// Build creates a 'resource_review_request' object using the configuration stored in the builder.
func (b *ResourceReviewRequestBuilder) Build() (object *ResourceReviewRequest, err error) {
	object = new(ResourceReviewRequest)
	object.bitmap_ = b.bitmap_
	object.accountUsername = b.accountUsername
	object.action = b.action
	object.reduceClusterList = b.reduceClusterList
	object.resourceType = b.resourceType
	return
}
