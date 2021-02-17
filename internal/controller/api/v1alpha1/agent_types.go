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
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AgentSyncedCondition   conditionsv1.ConditionType = "AgentSynced"
	AgentSyncedReason      string                     = "AgentSynced"
	AgentStateSynced       string                     = "Agent has been synced"
	AgentSyncErrorReason   string                     = "AgentSyncError"
	AgentStateFailedToSync string                     = "Failed to sync agent"
)

// AgentReference represents a Agent Reference. It has enough information to retrieve an agent
// in any namespace
type AgentReference struct {
	// Name is unique within a namespace to reference an agent resource.
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Namespace defines the space within which the agent name must be unique.
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
}

type HostMemory struct {
	PhysicalBytes int64 `json:"physicalBytes,omitempty"`
	UsableBytes   int64 `json:"usableBytes,omitempty"`
}

type HostCPU struct {
	Count int64 `json:"count,omitempty"`
	// Name in REST API: frequency
	ClockMegahertz int64    `json:"clockMegahertz,omitempty"`
	Flags          []string `json:"flags,omitempty"`
	ModelName      string   `json:"modelName,omitempty"`
	Architecture   string   `json:"architecture,omitempty"`
}

type HostInterface struct {
	IPV6Addresses []string `json:"ipV6Addresses"`
	Vendor        string   `json:"vendor,omitempty"`
	Name          string   `json:"name,omitempty"`
	HasCarrier    bool     `json:"hasCarrier,omitempty"`
	Product       string   `json:"product,omitempty"`
	Mtu           int64    `json:"mtu,omitempty"`
	IPV4Addresses []string `json:"ipV4Addresses"`
	Biosdevname   string   `json:"biosDevName,omitempty"`
	ClientId      string   `json:"clientID,omitempty"`
	MacAddress    string   `json:"macAddress,omitempty"`
	Flags         []string `json:"flags"`
	SpeedMbps     int64    `json:"speedMbps,omitempty"`
}

type HostInstallationEligibility struct {
	Eligible           bool     `json:"eligible,omitempty"`
	NotEligibleReasons []string `json:"notEligibleReasons"`
}

type HostIOPerf struct {
	// 99th percentile of fsync duration in milliseconds
	SyncDurationMilliseconds int64 `json:"syncDurationMilliseconds,omitempty"`
}

type HostDisk struct {
	DriveType               string                      `json:"driveType,omitempty"`
	Vendor                  string                      `json:"vendor,omitempty"`
	Name                    string                      `json:"name,omitempty"`
	Path                    string                      `json:"path,omitempty"`
	Hctl                    string                      `json:"hctl,omitempty"`
	ByPath                  string                      `json:"byPath,omitempty"`
	Model                   string                      `json:"model,omitempty"`
	Wwn                     string                      `json:"wwn,omitempty"`
	Serial                  string                      `json:"serial,omitempty"`
	SizeBytes               int64                       `json:"sizeBytes,omitempty"`
	Bootable                bool                        `json:"bootable,omitempty"`
	Smart                   string                      `json:"smart,omitempty"`
	InstallationEligibility HostInstallationEligibility `json:"installationEligibility,omitempty"`
	IoPerf                  HostIOPerf                  `json:"ioPerf,omitempty"`
}

type HostBoot struct {
	CurrentBootMode string `json:"currentBootMode,omitempty"`
	PxeInterface    string `json:"pxeInterface,omitempty"`
}

type HostSystemVendor struct {
	SerialNumber string `json:"serialNumber,omitempty"`
	ProductName  string `json:"productName,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Virtual      bool   `json:"virtual,omitempty"`
}

type HostInventory struct {
	// Name in REST API: timestamp
	ReportTime   *metav1.Time     `json:"reportTime,omitempty"`
	Hostname     string           `json:"hostname,omitempty"`
	BmcAddress   string           `json:"bmcAddress,omitempty"`
	BmcV6address string           `json:"bmcV6Address,omitempty"`
	Memory       HostMemory       `json:"memory,omitempty"`
	Cpu          HostCPU          `json:"cpu,omitempty"`
	Interfaces   []HostInterface  `json:"interfaces,omitempty"`
	Disks        []HostDisk       `json:"disks,omitempty"`
	Boot         HostBoot         `json:"boot,omitempty"`
	SystemVendor HostSystemVendor `json:"systemVendor,omitempty"`
}

// AgentSpec defines the desired state of Agent
type AgentSpec struct {
	ClusterDeploymentName   *ClusterReference `json:"clusterDeploymentName"`
	Role                    models.HostRole   `json:"role" protobuf:"bytes,1,opt,name=role,casttype=HostRole"`
	Hostname                string            `json:"hostname,omitempty"`
	MachineConfigPool       string            `json:"machineConfigPool,omitempty"`
	Enabled                 *bool             `json:"enabled,omitempty"`
	IgnitionConfigOverrides string            `json:"ignitionConfigOverrides,omitempty"`
	InstallerArgs           string            `json:"installerArgs,omitempty"`
}

type HardwareValidationInfo struct {
	HasInventory       host.ValidationStatus `json:"hasInventory,omitempty"`
	HasMinCPUCores     host.ValidationStatus `json:"hasMinCPUCores,omitempty"`
	HasMinMemory       host.ValidationStatus `json:"hasMinMemory,omitempty"`
	HasMinValidDisks   host.ValidationStatus `json:"hasMinValidDisks,omitempty"`
	HasCpuCoresForRole host.ValidationStatus `json:"hasCPUCoresForRole,omitempty"`
	HasMemoryForRole   host.ValidationStatus `json:"hasMemoryForRole,omitempty"`
	IsHostnameValid    host.ValidationStatus `json:"isHostnameValid,omitempty"`
	IsHostnameUnique   host.ValidationStatus `json:"isHostnameUnique,omitempty"`
	IsPlatformValid    host.ValidationStatus `json:"isPlatformValid,omitempty"`
}

type NetworkValidationInfo struct {
	Connected              host.ValidationStatus `json:"connected,omitempty"`
	MachineCidrDefined     host.ValidationStatus `json:"machineCIDRDefined,omitempty"`
	BelongsToMachineCidr   host.ValidationStatus `json:"belongsToMachineCIDR,omitempty"`
	APIVipConnected        host.ValidationStatus `json:"apiVIPConnected,omitempty"`
	BelongsToMajorityGroup host.ValidationStatus `json:"belongsToMajorityGroup,omitempty"`
	NTPSynced              host.ValidationStatus `json:"ntpSynced,omitempty"`
}

type HostValidationInfo struct {
	Hardware HardwareValidationInfo `json:"hardware,omitempty"`
	Network  NetworkValidationInfo  `json:"network,omitempty"`
}

type HostProgressInfo struct {
	CurrentStage models.HostStage `json:"currentStage,omitempty"`
	ProgressInfo string           `json:"progressInfo,omitempty"`
	// Name in REST API: stage_started_at
	StageStartTime string `json:"stageStartTime,omitempty"`
	// Name in REST API: stage_updated_at
	StageUpdateTime string `json:"stageUpdateTime,omitempty"`
}

type L2Connectivity struct {
	OutgoingIPAddress string `json:"outgoingIPAddress,omitempty"`
	OutgoingNic       string `json:"outgoingNIC,omitempty"`
	RemoteIPAddress   string `json:"remoteIPAddress,omitempty"`
	RemoteMac         string `json:"remoteMAC,omitempty"`
	Successful        bool   `json:"successful,omitempty"`
}

type L3Connectivity struct {
	OutgoingNic     string `json:"outgoingNIC,omitempty"`
	RemoteIPAddress string `json:"remoteIPAddress,omitempty"`
	Successful      bool   `json:"successful,omitempty"`
}

type HostConnectivityValidationInfo struct {
	HostDeploymentName *AgentReference `json:"hostDeploymentName"`
	L2Connectivity     L2Connectivity  `json:"l2Connectivity,omitempty"`
	L3Connectivity     L3Connectivity  `json:"l3Connectivity,omitempty"`
}

type HostNTPSources struct {
	SourceName  string             `json:"sourceName,omitempty"`
	SourceState models.SourceState `json:"sourceState,omitempty"`
}

// AgentStatus defines the observed state of Agent
type AgentStatus struct {
	State     string `json:"state,omitempty"`
	StateInfo string `json:"stateInfo,omitempty"`
	// Name in REST API: status_updated_at
	StateUpdatedTime *metav1.Time `json:"stateUpdatedTime,omitempty"`
	// Name in REST API: logs_collected_at
	LogsCollectedTime *metav1.Time `json:"logsCollectedTime,omitempty"`
	InstallerVersion  string       `json:"installerVersion,omitempty"`
	// Name in REST API: updated_at
	UpdateTime *metav1.Time `json:"updateTime,omitempty"`
	// Name in REST API: checked_in_at
	CheckedInTime         *metav1.Time                     `json:"checkedInTime,omitempty"`
	Hostname              string                           `json:"hostname,omitempty"`
	Bootstrap             bool                             `json:"bootstrap,omitempty"`
	DiscoveryAgentVersion string                           `json:"discoveryAgentVersion,omitempty"`
	Inventory             HostInventory                    `json:"inventory,omitempty"`
	ValidationInfo        HostValidationInfo               `json:"hostValidationInfo,omitempty"`
	Progress              HostProgressInfo                 `json:"progress,omitempty"`
	Connectivity          []HostConnectivityValidationInfo `json:"connectivity,omitempty"`
	APIVipConnectivity    bool                             `json:"apiVIPConnectivity,omitempty"`
	NtpSources            []HostNTPSources                 `json:"ntpSources,omitempty"`
	Conditions            []conditionsv1.Condition         `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Agent is the Schema for the hosts API
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
