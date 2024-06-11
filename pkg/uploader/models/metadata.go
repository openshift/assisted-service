package models

type Events struct {
	ClusterID string
	Metadata  *Metadata
}

func NewEvents(clusterID string) Events {
	return Events{
		ClusterID: clusterID,
	}
}

type Metadata struct {
	AssistedInstallerServiceVersion    string `json:"assisted-installer-service"`
	DiscoveryAgentVersion              string `json:"discovery-agent"`
	AssistedInstallerVersion           string `json:"assisted-installer"`
	AssistedInstallerControllerVersion string `json:"assisted-installer-controller"`

	DeploymentType    string `json:"deployment-type"`
	DeploymentVersion string `json:"deployment-version"`
	GitRef            string `json:"git-ref"`
}
