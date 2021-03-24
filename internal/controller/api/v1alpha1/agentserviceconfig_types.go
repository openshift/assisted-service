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
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentServiceConfigSpec defines the desired state of AgentServiceConfig
type AgentServiceConfigSpec struct {
	FileSystemStorage corev1.PersistentVolumeClaimSpec `json:"filesystemStorage"`
	DatabaseStorage   corev1.PersistentVolumeClaimSpec `json:"databaseStorage"`
}

// AgentServiceConfigStatus defines the observed state of AgentServiceConfig
type AgentServiceConfigStatus struct {
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// AgentServiceConfig represents an Assisted Service deployment
// +operator-sdk:csv:customresourcedefinitions:displayName="Agent Service Config"
type AgentServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentServiceConfigSpec   `json:"spec,omitempty"`
	Status AgentServiceConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentServiceConfigList contains a list of AgentServiceConfig
type AgentServiceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentServiceConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentServiceConfig{}, &AgentServiceConfigList{})
}
