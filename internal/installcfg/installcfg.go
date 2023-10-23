package installcfg

import (
	"github.com/go-openapi/strfmt"
	configv1 "github.com/openshift/api/config/v1"
	cluster_validations "github.com/openshift/assisted-service/internal/cluster/validations"
)

type Platform struct {
	Baremetal *BareMetalInstallConfigPlatform `yaml:"baremetal,omitempty"`
	None      *PlatformNone                   `yaml:"none,omitempty"`
	Vsphere   *VsphereInstallConfigPlatform   `yaml:"vsphere,omitempty"`
	Nutanix   *NutanixInstallConfigPlatform   `yaml:"nutanix,omitempty"`
	External  *ExternalInstallConfigPlatform  `yaml:"external,omitempty"`
}

type Host struct {
	Name           string `yaml:"name"`
	Role           string `yaml:"role"`
	BootMACAddress string `yaml:"bootMACAddress"`
	BootMode       string `yaml:"bootMode"`
}

type BareMetalInstallConfigPlatform struct {
	ProvisioningNetwork  string   `yaml:"provisioningNetwork"`
	APIVIPs              []string `yaml:"apiVIPs,omitempty"`
	DeprecatedAPIVIP     string   `yaml:"apiVIP,omitempty"`
	IngressVIPs          []string `yaml:"ingressVIPs,omitempty"`
	DeprecatedIngressVIP string   `yaml:"ingressVIP,omitempty"`
	Hosts                []Host   `yaml:"hosts"`
	ClusterOSImage       string   `json:"clusterOSImage,omitempty"`
}

type VsphereFailureDomainTopology struct {
	ComputeCluster string   `json:"computeCluster"`
	Datacenter     string   `json:"datacenter"`
	Datastore      string   `json:"datastore"`
	Folder         string   `json:"folder,omitempty"`
	Networks       []string `json:"networks,omitempty"`
	ResourcePool   string   `json:"resourcePool,omitempty"`
}

// VsphereFailureDomain holds the region and zone failure domain and the vCenter topology of that failure domain.
type VsphereFailureDomain struct {
	// Name defines the name of the VsphereFailureDomain. This name is arbitrary but will be used in VSpherePlatformDeploymentZone for association
	Name string `json:"name"`

	// Region defines a FailureDomainCoordinate which includes the name of the vCenter tag, the failure domain type and the name of the vCenter tag category.
	Region string `json:"region"`

	// Server is the fully-qualified domain name or the IP address of the vCenter server.
	Server string `json:"server"`

	// Topology describes a given failure domain using vSphere constructs
	Topology VsphereFailureDomainTopology `json:"topology"`

	// Zone defines a VSpherePlatformFailureDomain which includes the name of the vCenter tag, the failure domain type and the name of the vCenter tag category.
	Zone string `json:"zone"`
}

// VsphereVCenter stores the vCenter connection fields https://github.com/kubernetes/cloud-provider-vsphere/blob/master/pkg/common/config/types_yaml.go
type VsphereVCenter struct {
	// Datacenter in which VMs are located.
	Datacenters []string `json:"datacenters"`

	// Password is the password for the user to use
	Password strfmt.Password `json:"password"`

	// Port is the TCP port that will be used to communicate to the vCenter endpoint. This is typically unchanged
	// from the default of HTTPS TCP/443.
	Port int32 `json:"port,omitempty"`

	// Server is the fully-qualified domain name or the IP address of the vCenter server
	Server string `json:"server"`

	// Username is the username that will be used to connect to vCenter
	Username string `json:"user"`
}

type VsphereInstallConfigPlatform struct {
	DeprecatedVCenter          string                 `json:"vCenter,omitempty"`
	DeprecatedUsername         string                 `json:"username,omitempty"`
	DeprecatedPassword         strfmt.Password        `json:"password,omitempty"`
	DeprecatedDatacenter       string                 `json:"datacenter,omitempty"`
	DeprecatedDefaultDatastore string                 `json:"defaultDatastore,omitempty"`
	DeprecatedFolder           string                 `json:"folder,omitempty"`
	DeprecatedNetwork          string                 `json:"network,omitempty"`
	DeprecatedCluster          string                 `json:"cluster,omitempty"`
	DeprecatedAPIVIP           string                 `json:"apiVIP,omitempty"`
	DeprecatedIngressVIP       string                 `json:"ingressVIP,omitempty"`
	IngressVIPs                []string               `json:"ingressVIPs,omitempty"`
	APIVIPs                    []string               `json:"apiVIPs,omitempty"`
	FailureDomains             []VsphereFailureDomain `json:"failureDomains,omitempty"`
	VCenters                   []VsphereVCenter       `json:"vcenters,omitempty"`
}

type NutanixInstallConfigPlatform struct {
	ID                   int                   `yaml:"-"`
	APIVIPs              []string              `yaml:"apiVIPs,omitempty"`
	DeprecatedAPIVIP     string                `yaml:"apiVIP,omitempty"`
	IngressVIPs          []string              `yaml:"ingressVIPs,omitempty"`
	DeprecatedIngressVIP string                `yaml:"ingressVIP,omitempty"`
	PrismCentral         NutanixPrismCentral   `yaml:"prismCentral"`
	PrismElements        []NutanixPrismElement `yaml:"prismElements"`
	SubnetUUIDs          []strfmt.UUID         `yaml:"subnetUUIDs"`
}

type NutanixPrismCentral struct {
	ID                             int             `yaml:"-"`
	NutanixInstallConfigPlatformID int             `yaml:"-"`
	Endpoint                       NutanixEndpoint `yaml:"endpoint"`
	Username                       string          `yaml:"username"`
	Password                       strfmt.Password `yaml:"password"`
}

type NutanixEndpoint struct {
	ID                    int    `yaml:"-"`
	NutanixPrismCentralID int    `yaml:"-"`
	Address               string `yaml:"address"`
	Port                  int32  `yaml:"port"`
}

type NutanixPrismElement struct {
	ID                             int             `yaml:"-"`
	NutanixInstallConfigPlatformID int             `yaml:"-"`
	Endpoint                       NutanixEndpoint `yaml:"endpoint"`
	UUID                           strfmt.UUID     `yaml:"uuid"`
	Name                           string          `yaml:"name"`
}

type ExternalInstallConfigPlatform struct {
	PlatformName string `yaml:"platformName"`
}

type PlatformNone struct {
}

type BootstrapInPlace struct {
	InstallationDisk string `yaml:"installationDisk,omitempty"`
}

type Proxy struct {
	HTTPProxy  string `yaml:"httpProxy,omitempty"`
	HTTPSProxy string `yaml:"httpsProxy,omitempty"`
	NoProxy    string `yaml:"noProxy,omitempty"`
}

type ImageContentSource struct {
	Mirrors []string `yaml:"mirrors"`
	Source  string   `yaml:"source"`
}

type ClusterNetwork struct {
	Cidr       string `yaml:"cidr"`
	HostPrefix int    `yaml:"hostPrefix"`
}

type MachineNetwork struct {
	Cidr string `yaml:"cidr"`
}

type ClusterVersionCapabilitySet string

type ClusterVersionCapability string

type Capabilities struct {
	BaselineCapabilitySet         ClusterVersionCapabilitySet `yaml:"baselineCapabilitySet,omitempty"`
	AdditionalEnabledCapabilities []ClusterVersionCapability  `yaml:"additionalEnabledCapabilities,omitempty"`
}

type CPUPartitioningMode string

const (
	CPUPartitioningNone     CPUPartitioningMode = "None"
	CPUPartitioningAllNodes CPUPartitioningMode = "AllNodes"
)

type InstallerConfigBaremetal struct {
	APIVersion string `yaml:"apiVersion"`
	BaseDomain string `yaml:"baseDomain"`
	Proxy      *Proxy `yaml:"proxy,omitempty"`
	Networking struct {
		NetworkType    string           `yaml:"networkType"`
		ClusterNetwork []ClusterNetwork `yaml:"clusterNetwork"`
		MachineNetwork []MachineNetwork `yaml:"machineNetwork,omitempty"`
		ServiceNetwork []string         `yaml:"serviceNetwork"`
	} `yaml:"networking"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Compute []struct {
		Hyperthreading string `yaml:"hyperthreading,omitempty"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"compute"`
	ControlPlane struct {
		Hyperthreading string `yaml:"hyperthreading,omitempty"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"controlPlane"`
	Platform              Platform             `yaml:"platform"`
	BootstrapInPlace      BootstrapInPlace     `yaml:"bootstrapInPlace,omitempty"`
	FIPS                  bool                 `yaml:"fips"`
	CPUPartitioningMode   CPUPartitioningMode  `yaml:"cpuPartitioningMode,omitempty"`
	PullSecret            string               `yaml:"pullSecret"`
	SSHKey                string               `yaml:"sshKey"`
	AdditionalTrustBundle string               `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []ImageContentSource `yaml:"imageContentSources,omitempty"`
	Capabilities          *Capabilities        `yaml:"capabilities,omitempty"`
	FeatureSet            configv1.FeatureSet  `yaml:"featureSet,omitempty"`
}

func (c *InstallerConfigBaremetal) Validate() error {
	if c.AdditionalTrustBundle != "" {
		return cluster_validations.ValidatePEMCertificateBundle(c.AdditionalTrustBundle)
	}

	return nil
}
