package installcfg

import (
	"github.com/go-openapi/strfmt"
	cluster_validations "github.com/openshift/assisted-service/internal/cluster/validations"
)

type Platform struct {
	Baremetal *BareMetalInstallConfigPlatform `yaml:"baremetal,omitempty"`
	None      *PlatformNone                   `yaml:"none,omitempty"`
	Vsphere   *VsphereInstallConfigPlatform   `yaml:"vsphere,omitempty"`
	Nutanix   *NutanixInstallConfigPlatform   `yaml:"nutanix,omitempty"`
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
	ComputeCluster string   `yaml:"computeCluster"`
	Datacenter     string   `yaml:"datacenter"`
	Datastore      string   `yaml:"datastore"`
	Folder         string   `yaml:"folder,omitempty"`
	Networks       []string `yaml:"networks,omitempty"`
	ResourcePool   string   `yaml:"resourcePool,omitempty"`
}

// VsphereFailureDomain holds the region and zone failure domain and the vCenter topology of that failure domain.
type VsphereFailureDomain struct {
	// Name defines the name of the VsphereFailureDomain. This name is arbitrary but will be used in VSpherePlatformDeploymentZone for association
	Name string `yaml:"name"`

	// Region defines a FailureDomainCoordinate which includes the name of the vCenter tag, the failure domain type and the name of the vCenter tag category.
	Region string `yaml:"region"`

	// Server is the fully-qualified domain name or the IP address of the vCenter server.
	Server string `yaml:"server"`

	// Topology describes a given failure domain using vSphere constructs
	Topology VsphereFailureDomainTopology `yaml:"topology"`

	// Zone defines a VSpherePlatformFailureDomain which includes the name of the vCenter tag, the failure domain type and the name of the vCenter tag category.
	Zone string `yaml:"zone"`
}

// VsphereVCenter stores the vCenter connection fields https://github.com/kubernetes/cloud-provider-vsphere/blob/master/pkg/common/config/types_yaml.go
type VsphereVCenter struct {
	// Datacenter in which VMs are located.
	Datacenters []string `yaml:"datacenters"`

	// Password is the password for the user to use
	Password strfmt.Password `yaml:"password"`

	// Port is the TCP port that will be used to communicate to the vCenter endpoint. This is typically unchanged
	// from the default of HTTPS TCP/443.
	Port int32 `yaml:"port,omitempty"`

	// Server is the fully-qualified domain name or the IP address of the vCenter server
	Server string `yaml:"server"`

	// Username is the username that will be used to connect to vCenter
	Username string `yaml:"user"`
}

type VsphereInstallConfigPlatform struct {
	DeprecatedVCenter          string                 `yaml:"vCenter,omitempty"`
	DeprecatedUsername         string                 `yaml:"username,omitempty"`
	DeprecatedPassword         strfmt.Password        `yaml:"password,omitempty"`
	DeprecatedDatacenter       string                 `yaml:"datacenter,omitempty"`
	DeprecatedDefaultDatastore string                 `yaml:"defaultDatastore,omitempty"`
	DeprecatedFolder           string                 `yaml:"folder,omitempty"`
	DeprecatedNetwork          string                 `yaml:"network,omitempty"`
	DeprecatedCluster          string                 `yaml:"cluster,omitempty"`
	DeprecatedAPIVIP           string                 `yaml:"apiVIP,omitempty"`
	DeprecatedIngressVIP       string                 `yaml:"ingressVIP,omitempty"`
	IngressVIPs                []string               `yaml:"ingressVIPs,omitempty"`
	APIVIPs                    []string               `yaml:"apiVIPs,omitempty"`
	FailureDomains             []VsphereFailureDomain `yaml:"failureDomains,omitempty"`
	VCenters                   []VsphereVCenter       `yaml:"vcenters,omitempty"`
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
	CPUPartitioning       CPUPartitioningMode  `json:"cpuPartitioningMode,omitempty"`
	PullSecret            string               `yaml:"pullSecret"`
	SSHKey                string               `yaml:"sshKey"`
	AdditionalTrustBundle string               `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []ImageContentSource `yaml:"imageContentSources,omitempty"`
	Capabilities          *Capabilities        `yaml:"capabilities,omitempty"`
}

func (c *InstallerConfigBaremetal) Validate() error {
	if c.AdditionalTrustBundle != "" {
		return cluster_validations.ValidatePEMCertificateBundle(c.AdditionalTrustBundle)
	}

	return nil
}
