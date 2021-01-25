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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RawConfig []byte

type NMStateConfigSpec struct {
	// MACAddress is a MAC address for any network device on the host to which this config
	// should be applied. This value is only used to ensure that the config is applied to the
	// intended host.
	MACAddress string `json:"macAddress"`

	//Config contains the  yaml [1] as string instead of golang struct so we don't need to be in
	//sync with the schema.
	//
	// [1] https://github.com/nmstate/nmstate/blob/base/libnmstate/schemas/operational-state.yaml
	Config RawConfig `json:"-"`
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
