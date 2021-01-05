/*
Copyright 2020.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Cluster name
	Name string `json:"name"`
	// Installation will start once cluster is in known state and approved
	Approved                 bool   `json:"approved,omitempty"`
	OpenshiftVersion         string `json:"openshiftVersion"`
	BaseDnsDomain            string `json:"baseDnsDomain,omitempty"`
	ClusterNetworkCidr       string `json:"clusterNetworkCidr,omitempty"`
	ClusterNetworkHostPrefix int64  `json:"clusterNetworkHostPrefix,omitempty"`
	ServiceNetworkCidr       string `json:"serviceNetworkCidr,omitempty"`
	ApiVip                   string `json:"apiVip,omitempty"`
	ApiVipDnsName            string `json:"apiVipDnsName,omitempty"`
	IngressVip               string `json:"ingresVip,omitempty"`
	MachineNetworkCidr       string `json:"machineNetworkCidr,omitempty"`
	SshPublicKey             string `json:"sshPublicKey,omitempty"`
	VipDhcpAllocation        bool   `json:"vipDhcpAllocation,omitempty"`
	HttpProxy                string `json:"httpProxy,omitempty"`
	HttpsProxy               string `json:"httpsProxy,omitempty"`
	NoProxy                  string `json:"noProxy,omitempty"`
	UserManagedNetworking    bool   `json:"userManagedNetworking,omitempty"`
	AdditionalNtpSource      string `json:"additionalNtpSource,omitempty"`
	InstallConfigOverrides   string `json:"installConfigOverrides,omitempty"`
	// A reference to the secret containing the pull secret
	PullSecretRef *corev1.SecretReference `json:"pullSecretRef"`
}

type ClusterProgressInfo struct {
	// progress info
	ProgressInfo string `json:"progressInfo,omitempty"`
	// Time at which the cluster install progress was last updated.
	LastProgressUpdateTime *metav1.Time `json:"lastProgressUpdateTime,omitempty"`
}

type HostNetwork struct {
	// cidr
	Cidr string `json:"cidr,omitempty"`
	// host ids
	HostIds []string `json:"hostIDs"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	State                        string              `json:"state,omitempty"`
	StateInfo                    string              `json:"stateInfo,omitempty"`
	HostNetworks                 []HostNetwork       `json:"hostNetworks,omitempty"`
	InstallationStartTime        string              `json:"installationStartTime,omitempty"`
	InstallationCompletionTime   *metav1.Time        `json:"installationCompletionTime,omitempty"`
	Hosts                        int                 `json:"hosts,omitempty"`
	Progress                     ClusterProgressInfo `json:"progress,omitempty"`
	ValidationsInfo              string              `json:"validationsInfo,omitempty"`
	ConnectivityMajorityGroups   string              `json:"connectivityMajorityGroups,omitempty"`
	LastUpdateTime               *metav1.Time        `json:"lastUpdateTime,omitempty"`
	ControllerLogsCollectionTime *metav1.Time        `json:"controllerLogsCollectionTime,omitempty"`
	// Display api errors
	Error string `json:"error,omitempty"`
	// Cluster ID
	// TODO: probably need to work with labels
	ID string `json:"id,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
