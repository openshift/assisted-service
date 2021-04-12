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
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ImageCreatedReason       = "ImageCreated"
	ImageStateCreated        = "Image has been created"
	ImageCreationErrorReason = "ImageCreationError"
	ImageStateFailedToCreate = "Failed to create image"
)

// ClusterReference represents a Cluster Reference. It has enough information to retrieve cluster
// in any namespace
type ClusterReference struct {
	// Name is unique within a namespace to reference a cluster resource.
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace defines the space within which the cluster name must be unique.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

const (
	ImageCreatedCondition conditionsv1.ConditionType = "ImageCreated"
)

type InstallEnvSpec struct {
	// Proxy defines the proxy settings for agents and clusters that use the InstallEnv. If
	// unset, the agents and clusters will not be configured to use a proxy.
	// +optional
	Proxy *Proxy `json:"proxy,omitempty"`

	// AdditionalNTPSources is a list of NTP sources (hostname or IP) to be added to all cluster
	// hosts. They are added to any NTP sources that were configured through other means.
	// +optional
	AdditionalNTPSources []string `json:"additionalNTPSources,omitempty"`

	// SSHAuthorizedKey is a SSH public keys that will be added to all agents for use in debugging.
	// +optional
	SSHAuthorizedKey string `json:"sshAuthorizedKey,omitempty"`

	// PullSecretRef is the reference to the secret to use when pulling images.
	PullSecretRef *corev1.LocalObjectReference `json:"pullSecretRef"`

	// AgentLabelSelector specifies a label that should be applied to Agents that boot from the
	// installation media of this InstallEnv. This is how a user would identify which agents are
	// associated with a particular InstallEnv.
	AgentLabelSelector metav1.LabelSelector `json:"agentLabelSelector"`

	// AgentLabels lists labels to apply to Agents that are associated with this InstallEnv upon
	// the creation of the Agents.
	// +optional
	AgentLabels map[string]string `json:"agentLabels,omitempty"`

	// NmstateConfigLabelSelector associates NMStateConfigs for hosts that are considered part
	// of this installation environment.
	// +optional
	NMStateConfigLabelSelector metav1.LabelSelector `json:"nmStateConfigLabelSelector,omitempty"`

	// ClusterRef is the reference to the single ClusterDeployment that will be installed from
	// this InstallEnv.
	// Future versions will allow for multiple ClusterDeployments and this reference will be
	// removed.
	ClusterRef *ClusterReference `json:"clusterRef"`
	// Json formatted string containing the user overrides for the initial ignition config
	// +optional
	IgnitionConfigOverride string `json:"ignitionConfigOverride,omitempty"`
}

// Proxy defines the proxy settings for agents and clusters that use the InstallEnv.
// At least one of HTTPProxy or HTTPSProxy is required.
type Proxy struct {
	// HTTPProxy is the URL of the proxy for HTTP requests.
	// +optional
	HTTPProxy string `json:"httpProxy,omitempty"`

	// HTTPSProxy is the URL of the proxy for HTTPS requests.
	// +optional
	HTTPSProxy string `json:"httpsProxy,omitempty"`

	// NoProxy is a comma-separated list of domains and CIDRs for which the proxy should not be
	// used.
	// +optional
	NoProxy string `json:"noProxy,omitempty"`
}

type InstallEnvStatus struct {
	// ISODownloadURL specifies an HTTP/S URL that contains a discovery ISO containing the
	// configuration from this InstallEnv.
	ISODownloadURL string                   `json:"isoDownloadURL,omitempty"`
	Conditions     []conditionsv1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type InstallEnv struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallEnvSpec   `json:"spec,omitempty"`
	Status InstallEnvStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstallEnvList contains a list of InstallEnvs
type InstallEnvList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstallEnv `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InstallEnv{}, &InstallEnvList{})
}
