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

// IngressBuilder contains the data and logic needed to build 'ingress' objects.
//
// Representation of an ingress.
type IngressBuilder struct {
	bitmap_        uint32
	id             string
	href           string
	dnsName        string
	cluster        *ClusterBuilder
	listening      ListeningMethod
	routeSelectors map[string]string
	default_       bool
}

// NewIngress creates a new builder of 'ingress' objects.
func NewIngress() *IngressBuilder {
	return &IngressBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *IngressBuilder) Link(value bool) *IngressBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *IngressBuilder) ID(value string) *IngressBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *IngressBuilder) HREF(value string) *IngressBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// DNSName sets the value of the 'DNS_name' attribute to the given value.
//
//
func (b *IngressBuilder) DNSName(value string) *IngressBuilder {
	b.dnsName = value
	b.bitmap_ |= 8
	return b
}

// Cluster sets the value of the 'cluster' attribute to the given value.
//
// Definition of an _OpenShift_ cluster.
//
// The `cloud_provider` attribute is a reference to the cloud provider. When a
// cluster is retrieved it will be a link to the cloud provider, containing only
// the kind, id and href attributes:
//
// [source,json]
// ----
// {
//   "cloud_provider": {
//     "kind": "CloudProviderLink",
//     "id": "123",
//     "href": "/api/clusters_mgmt/v1/cloud_providers/123"
//   }
// }
// ----
//
// When a cluster is created this is optional, and if used it should contain the
// identifier of the cloud provider to use:
//
// [source,json]
// ----
// {
//   "cloud_provider": {
//     "id": "123",
//   }
// }
// ----
//
// If not included, then the cluster will be created using the default cloud
// provider, which is currently Amazon Web Services.
//
// The region attribute is mandatory when a cluster is created.
//
// The `aws.access_key_id`, `aws.secret_access_key` and `dns.base_domain`
// attributes are mandatory when creation a cluster with your own Amazon Web
// Services account.
func (b *IngressBuilder) Cluster(value *ClusterBuilder) *IngressBuilder {
	b.cluster = value
	if value != nil {
		b.bitmap_ |= 16
	} else {
		b.bitmap_ &^= 16
	}
	return b
}

// Default sets the value of the 'default' attribute to the given value.
//
//
func (b *IngressBuilder) Default(value bool) *IngressBuilder {
	b.default_ = value
	b.bitmap_ |= 32
	return b
}

// Listening sets the value of the 'listening' attribute to the given value.
//
// Cluster components listening method.
func (b *IngressBuilder) Listening(value ListeningMethod) *IngressBuilder {
	b.listening = value
	b.bitmap_ |= 64
	return b
}

// RouteSelectors sets the value of the 'route_selectors' attribute to the given value.
//
//
func (b *IngressBuilder) RouteSelectors(value map[string]string) *IngressBuilder {
	b.routeSelectors = value
	if value != nil {
		b.bitmap_ |= 128
	} else {
		b.bitmap_ &^= 128
	}
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *IngressBuilder) Copy(object *Ingress) *IngressBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.dnsName = object.dnsName
	if object.cluster != nil {
		b.cluster = NewCluster().Copy(object.cluster)
	} else {
		b.cluster = nil
	}
	b.default_ = object.default_
	b.listening = object.listening
	if len(object.routeSelectors) > 0 {
		b.routeSelectors = map[string]string{}
		for k, v := range object.routeSelectors {
			b.routeSelectors[k] = v
		}
	} else {
		b.routeSelectors = nil
	}
	return b
}

// Build creates a 'ingress' object using the configuration stored in the builder.
func (b *IngressBuilder) Build() (object *Ingress, err error) {
	object = new(Ingress)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.dnsName = b.dnsName
	if b.cluster != nil {
		object.cluster, err = b.cluster.Build()
		if err != nil {
			return
		}
	}
	object.default_ = b.default_
	object.listening = b.listening
	if b.routeSelectors != nil {
		object.routeSelectors = make(map[string]string)
		for k, v := range b.routeSelectors {
			object.routeSelectors[k] = v
		}
	}
	return
}
