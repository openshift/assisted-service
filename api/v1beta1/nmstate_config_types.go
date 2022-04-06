/*
Copyright 2021.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Interface struct {
	// nic name used in the yaml, which relates 1:1 to the mac address.
	// Name in REST API: logicalNICName
	Name string `json:"name"`
	// mac address present on the host.
	// +kubebuilder:validation:Pattern=`^([0-9A-Fa-f]{2}[:]){5}([0-9A-Fa-f]{2})$`
	MacAddress string `json:"macAddress"`
}

type Address struct {
	IP           string `json:"ip,omitempty"`
	PrefixLength string `json:"prefix-length,omitempty"`
}

type IPV4 struct {
	Enabled string    `json:"enabled,omitempty"`
	Address []Address `json:"address,omitempty"`
	DHCP    string    `json:"dhcp,omitempty"`
}

type IPV6 struct {
	Enabled  string    `json:"enabled,omitempty"`
	Address  []Address `json:"address,omitempty"`
	AutoConf string    `json:"autoconf,omitempty"`
	DHCP     string    `json:"dhcp,omitempty"`
}

type NetConfigInterfaces struct {
	Name       string `json:"name,omitempty"`
	Type       string `json:"type,omitempty"`
	State      string `json:"state,omitempty"`
	MacAddress string `json:"mac-address,omitempty"`
	IPV4       IPV4   `json:ipv4,omitempty`
	IPV6       IPV6   `json:ipv6,omitempty`
}

type DNSConfig struct {
	Server []string `json:"server,omitempty"`
}

type DNSResolver struct {
	DNSConfig DNSConfig `json:"config,omitempty"`
}

type RoutesConfig struct {
	Destination      string `json:"destination,omitempty"`
	NextHopAddress   string `json:"next-hop-address,omitempty"`
	NextHopInterface string `json:"next-hop-interface,omitempty"`
	TableID          string `json:"table-id,omitempty"`
}

type NetConfigRoutes struct {
	Config []RoutesConfig `json:"config,omitempty"`
}

type NetConfig struct {
	Interfaces  []NetConfigInterfaces `json:"interfaces,omitempty"`
	DNSResolver DNSResolver           `json:"dns-resolver,omitempty"`
	Routes      NetConfigRoutes       `json:"routes,omitempty"`
}

type NMStateConfigSpec struct {
	// Interfaces is an array of interface objects containing the name and MAC
	// address for interfaces that are referenced in the raw nmstate config YAML.
	// Interfaces listed here will be automatically renamed in the nmstate config
	// YAML to match the real device name that is observed to have the
	// corresponding MAC address. At least one interface must be listed so that it
	// can be used to identify the correct host, which is done by matching any MAC
	// address in this list to any MAC address observed on the host.
	// +kubebuilder:validation:MinItems=1
	Interfaces []*Interface `json:"interfaces,omitempty"`
	// yaml that can be processed by nmstate, using custom marshaling/unmarshaling that will allow to populate nmstate config as plain yaml.
	// +kubebuilder:validation:XPreserveUnknownFields
	NetConfig NetConfig `json:"config,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type NMStateConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NMStateConfigSpec `json:"spec,omitempty"`
	// No status
}

// +kubebuilder:object:root=true

// NMStateConfigList contains a list of NMStateConfigs
type NMStateConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NMStateConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NMStateConfig{}, &NMStateConfigList{})
}

// This override the NetConfig type [1] so we can do a custom marshalling of
// nmstate yaml without the need to have golang code representing the nmstate schema

// [1] https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
func (_ NetConfig) OpenAPISchemaType() []string { return []string{"object"} }
