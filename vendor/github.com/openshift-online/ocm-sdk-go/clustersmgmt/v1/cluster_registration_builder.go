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

// ClusterRegistrationBuilder contains the data and logic needed to build 'cluster_registration' objects.
//
// Registration of a new cluster to the service.
type ClusterRegistrationBuilder struct {
	bitmap_        uint32
	externalID     string
	organizationID string
	subscriptionID string
}

// NewClusterRegistration creates a new builder of 'cluster_registration' objects.
func NewClusterRegistration() *ClusterRegistrationBuilder {
	return &ClusterRegistrationBuilder{}
}

// ExternalID sets the value of the 'external_ID' attribute to the given value.
//
//
func (b *ClusterRegistrationBuilder) ExternalID(value string) *ClusterRegistrationBuilder {
	b.externalID = value
	b.bitmap_ |= 1
	return b
}

// OrganizationID sets the value of the 'organization_ID' attribute to the given value.
//
//
func (b *ClusterRegistrationBuilder) OrganizationID(value string) *ClusterRegistrationBuilder {
	b.organizationID = value
	b.bitmap_ |= 2
	return b
}

// SubscriptionID sets the value of the 'subscription_ID' attribute to the given value.
//
//
func (b *ClusterRegistrationBuilder) SubscriptionID(value string) *ClusterRegistrationBuilder {
	b.subscriptionID = value
	b.bitmap_ |= 4
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *ClusterRegistrationBuilder) Copy(object *ClusterRegistration) *ClusterRegistrationBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.externalID = object.externalID
	b.organizationID = object.organizationID
	b.subscriptionID = object.subscriptionID
	return b
}

// Build creates a 'cluster_registration' object using the configuration stored in the builder.
func (b *ClusterRegistrationBuilder) Build() (object *ClusterRegistration, err error) {
	object = new(ClusterRegistration)
	object.bitmap_ = b.bitmap_
	object.externalID = b.externalID
	object.organizationID = b.organizationID
	object.subscriptionID = b.subscriptionID
	return
}
