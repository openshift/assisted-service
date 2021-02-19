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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ImageStateCreated        = "Image has been created"
	ImageStateFailedToCreate = "Failed to create image"
)

// ImageSpec defines the desired state of Image
type ImageSpec struct {
	// The name of the Cluster CR
	ClusterRef   *ClusterReference `json:"clusterRef"`
	SSHPublicKey string            `json:"sshPublicKey,omitempty"`
	// The name of the secret containing the pull secret
	IgnitionOverrides string `json:"ignitionOverrides,omitempty"`
}

// ImageStatus defines the observed state of Image
type ImageStatus struct {
	State          string       `json:"state,omitempty"`
	SizeBytes      int          `json:"sizeBytes,omitempty"`
	DownloadUrl    string       `json:"downloadUrl,omitempty"`
	ExpirationTime *metav1.Time `json:"expirationTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Image is the Schema for the images API
type Image struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpec   `json:"spec,omitempty"`
	Status ImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageList contains a list of Image
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Image{}, &ImageList{})
}
