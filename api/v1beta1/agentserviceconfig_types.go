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

// OSImage defines an Operating System image and the OpenShift version it
// is associated with.
type OSImage struct {
	// OpenshiftVersion is the Major.Minor version of OpenShift that this image
	// is to be associated with.
	OpenshiftVersion string `json:"openshiftVersion"`
	// Version is the Operating System version of the image.
	Version string `json:"version"`
	// Url specifies the path to the Operating System image.
	Url string `json:"url"`
	// rootFSUrl specifies the path to the root filesystem.
	RootFSUrl string `json:"rootFSUrl"`
}

// AgentServiceConfigSpec defines the desired state of AgentServiceConfig
type AgentServiceConfigSpec struct {
	// FileSystemStorage defines the spec of the PersistentVolumeClaim to be
	// created for the assisted-service's filesystem (logs, etc).
	// With respect to the resource requests, the amount of filesystem storage
	// consumer will depend largely on the number of clusters you intend to create.
	// Approximate storage requiremens include:
	//   - ~200 MB per cluster
	//   - ~2-3 GB per supported OpenShift version
	// 20Gi is the recommended minimum for development/testing and 100Gi is recommended
	// for everything else.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage for service filesystem"
	FileSystemStorage corev1.PersistentVolumeClaimSpec `json:"filesystemStorage"`
	// DatabaseStorage defines the spec of the PersistentVolumeClaim to be
	// created for the database's filesystem.
	// With respect to the resource requests, a minimum of 10Gi is recommended.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage for database"
	DatabaseStorage corev1.PersistentVolumeClaimSpec `json:"databaseStorage"`
	// MirrorRegistryRef is the reference to the configmap that contains mirror registry configuration
	// In case no configuration is need, this field will be nil. ConfigMap must contain to entries:
	// ca-bundle.crt - hold the contents of mirror registry certificate/s
	// registries.conf - holds the content of registries.conf file configured with mirror registries
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Mirror Registry and Certificate ConfigMap Name"
	MirrorRegistryRef *corev1.LocalObjectReference `json:"mirrorRegistryRef,omitempty"`

	// OSImages defines a collection of Operating System images (ie. RHCOS images)
	// that the assisted-service should use as the base when generating discovery ISOs.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Operating System Images"
	OSImages []OSImage `json:"osImages,omitempty"`
}

// ConditionType related to our reconcile loop in addition to all the reasons
// why ConditionStatus could be true or false.
const (
	// ConditionReconcileCompleted reports whether reconcile completed without error.
	ConditionReconcileCompleted conditionsv1.ConditionType = "ReconcileCompleted"

	// ReasonReconcileSucceeded when the reconcile completes all operations without error.
	ReasonReconcileSucceeded string = "ReconcileSucceeded"
	// ReasonStorageFailure when there was a failure configuring/deploying storage.
	ReasonStorageFailure string = "StorageFailure"
	// ReasonAgentServiceFailure when there was a failure related to the assisted-service's service.
	ReasonAgentServiceFailure string = "AgentServiceFailure"
	// ReasonAgentRouteFailure when there was a failure configuring/deploying the assisted-service's route.
	ReasonAgentRouteFailure string = "AgentRouteFailure"
	// ReasonAgentLocalAuthSecretFailure when there was a failure generating/deploying the local auth key pair secret.
	ReasonAgentLocalAuthSecretFailure string = "AgentLocalAuthSecretFailure" // #nosec
	// ReasonPostgresSecretFailure when there was a failure generating/deploying the database secret.
	ReasonPostgresSecretFailure string = "PostgresSecretFailure"
	// ReasonDeploymentFailure when there was a failure configuring/deploying the assisted-service deployment.
	ReasonDeploymentFailure string = "DeploymentFailure"
)

// AgentServiceConfigStatus defines the observed state of AgentServiceConfig
type AgentServiceConfigStatus struct {
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// AgentServiceConfig represents an Assisted Service deployment.
// Only an AgentServiceConfig with name="agent" will be reconciled, for all other
// names a "Warning" event will be emitted + message logged and it will be ignored
// forever.
// +operator-sdk:csv:customresourcedefinitions:displayName="Agent Service Config"
// +operator-sdk:csv:customresourcedefinitions:order=1
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
