package installcfg

import (
	"crypto/x509"
	"encoding/pem"

	"github.com/go-openapi/strfmt"
	"github.com/pkg/errors"
)

type Platform struct {
	Baremetal *BareMetalInstallConfigPlatform `yaml:"baremetal,omitempty"`
	None      *PlatformNone                   `yaml:"none,omitempty"`
	Ovirt     *OvirtInstallConfigPlatform     `yaml:"ovirt,omitempty"`
	Vsphere   *VsphereInstallConfigPlatform   `yaml:"vsphere"`
}

type Host struct {
	Name           string `yaml:"name"`
	Role           string `yaml:"role"`
	BootMACAddress string `yaml:"bootMACAddress"`
	BootMode       string `yaml:"bootMode"`
}

type BareMetalInstallConfigPlatform struct {
	ProvisioningNetwork string `yaml:"provisioningNetwork"`
	APIVIP              string `yaml:"apiVIP"`
	IngressVIP          string `yaml:"ingressVIP"`
	Hosts               []Host `yaml:"hosts"`
	ClusterOSImage      string `json:"clusterOSImage,omitempty"`
}

type VsphereInstallConfigPlatform struct {
	VCenter          string          `yaml:"vCenter"`
	Username         string          `yaml:"username"`
	Password         strfmt.Password `yaml:"password"`
	Datacenter       string          `yaml:"datacenter"`
	DefaultDatastore string          `yaml:"defaultDatastore"`
	Folder           string          `yaml:"folder,omitempty"`
	Network          string          `yaml:"network"`
	Cluster          string          `yaml:"cluster"`
	APIVIP           string          `yaml:"apiVIP"`
	IngressVIP       string          `yaml:"ingressVIP"`
}

// OvirtInstallConfigPlatform represents the required parameters
// within the `install-config.yaml` for the oVirt platform.
type OvirtInstallConfigPlatform struct {
	APIVIP          string      `yaml:"api_vip"`
	IngressVIP      string      `yaml:"ingress_vip"`
	ClusterID       strfmt.UUID `yaml:"ovirt_cluster_id"`
	StorageDomainID strfmt.UUID `yaml:"ovirt_storage_domain_id"`
	NetworkName     string      `yaml:"ovirt_network_name"`
	VnicProfileID   strfmt.UUID `yaml:"vnicProfileID"`
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
	PullSecret            string               `yaml:"pullSecret"`
	SSHKey                string               `yaml:"sshKey"`
	AdditionalTrustBundle string               `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []ImageContentSource `yaml:"imageContentSources,omitempty"`
	Capabilities          *Capabilities        `yaml:"capabilities,omitempty"`
}

func (c *InstallerConfigBaremetal) Validate() error {
	if c.AdditionalTrustBundle != "" {
		// From https://github.com/openshift/installer/blob/56e61f1df5aa51ff244465d4bebcd1649003b0c9/pkg/validate/validate.go#L29-L47
		rest := []byte(c.AdditionalTrustBundle)
		for {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				return errors.Errorf("invalid block")
			}
			_, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return err
			}
			if len(rest) == 0 {
				break
			}
		}
	}

	return nil
}
