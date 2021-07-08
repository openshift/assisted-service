package installcfg

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v2"
)

type host struct {
	Name           string `yaml:"name"`
	Role           string `yaml:"role"`
	BootMACAddress string `yaml:"bootMACAddress"`
	BootMode       string `yaml:"bootMode"`
}

type baremetal struct {
	ProvisioningNetwork string `yaml:"provisioningNetwork"`
	APIVIP              string `yaml:"apiVIP"`
	IngressVIP          string `yaml:"ingressVIP"`
	Hosts               []host `yaml:"hosts"`
}

type platform struct {
	Baremetal *baremetal       `yaml:"baremetal,omitempty"`
	None      *platformNone    `yaml:"none,omitempty"`
	Vsphere   *platformVsphere `yaml:"vsphere,omitempty"`
}

type platformVsphere struct {
	VCenter          string `yaml:"vCenter"`
	Username         string `yaml:"username"`
	Password         string `yaml:"password"`
	Datacenter       string `yaml:"datacenter"`
	DefaultDatastore string `yaml:"defaultDatastore"`
	Folder           string `yaml:"folder,omitempty"`
	Network          string `yaml:"network"`
	Cluster          string `yaml:"cluster"`
	APIVIP           string `yaml:"apiVIP"`
	IngressVIP       string `yaml:"ingressVIP"`
}

type platformNone struct {
}

type bootstrapInPlace struct {
	InstallationDisk string `yaml:"installationDisk,omitempty"`
}

type proxy struct {
	HTTPProxy  string `yaml:"httpProxy,omitempty"`
	HTTPSProxy string `yaml:"httpsProxy,omitempty"`
	NoProxy    string `yaml:"noProxy,omitempty"`
}

type imageContentSource struct {
	Mirrors []string `yaml:"mirrors"`
	Source  string   `yaml:"source"`
}

type InstallerConfigBaremetal struct {
	APIVersion string `yaml:"apiVersion"`
	BaseDomain string `yaml:"baseDomain"`
	Proxy      *proxy `yaml:"proxy,omitempty"`
	Networking struct {
		NetworkType    string `yaml:"networkType"`
		ClusterNetwork []struct {
			Cidr       string `yaml:"cidr"`
			HostPrefix int    `yaml:"hostPrefix"`
		} `yaml:"clusterNetwork"`
		MachineNetwork []struct {
			Cidr string `yaml:"cidr"`
		} `yaml:"machineNetwork,omitempty"`
		ServiceNetwork []string `yaml:"serviceNetwork"`
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
	Platform              platform             `yaml:"platform"`
	BootstrapInPlace      bootstrapInPlace     `yaml:"bootstrapInPlace,omitempty"`
	FIPS                  bool                 `yaml:"fips"`
	PullSecret            string               `yaml:"pullSecret"`
	SSHKey                string               `yaml:"sshKey"`
	AdditionalTrustBundle string               `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []imageContentSource `yaml:"imageContentSources,omitempty"`
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

//go:generate mockgen -source=installcfg.go -package=installcfg -destination=mock_installcfg.go
type InstallConfigBuilder interface {
	GetInstallConfig(cluster *common.Cluster, addRhCa bool, ca string) ([]byte, error)
	ValidateInstallConfigPatch(cluster *common.Cluster, patch string) error
}

type installConfigBuilder struct {
	log                     logrus.FieldLogger
	mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder
}

func NewInstallConfigBuilder(log logrus.FieldLogger, mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder) InstallConfigBuilder {
	return &installConfigBuilder{log: log, mirrorRegistriesBuilder: mirrorRegistriesBuilder}
}

func (i *installConfigBuilder) countHostsByRole(cluster *common.Cluster, role models.HostRole) int {
	var count int
	for _, host := range cluster.Hosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled && host.Role == role {
			count += 1
		}
	}
	return count
}

func (i *installConfigBuilder) getNetworkType(cluster *common.Cluster) string {
	networkType := "OpenShiftSDN"
	if network.IsIPv6CIDR(cluster.ClusterNetworkCidr) || network.IsIPv6CIDR(cluster.MachineNetworkCidr) || network.IsIPv6CIDR(cluster.ServiceNetworkCidr) {
		networkType = "OVNKubernetes"
	}
	return networkType
}

func (i *installConfigBuilder) generateNoProxy(cluster *common.Cluster) string {
	noProxy := strings.TrimSpace(cluster.NoProxy)
	if noProxy == "*" {
		return noProxy
	}

	splitNoProxy := funk.FilterString(strings.Split(noProxy, ","), func(s string) bool { return s != "" })
	if cluster.MachineNetworkCidr != "" {
		splitNoProxy = append(splitNoProxy, cluster.MachineNetworkCidr)
	}
	// Add internal OCP DNS domain
	internalDnsDomain := "." + cluster.Name + "." + cluster.BaseDNSDomain
	return strings.Join(append(splitNoProxy, internalDnsDomain, cluster.ClusterNetworkCidr, cluster.ServiceNetworkCidr), ",")
}

func (i *installConfigBuilder) getBasicInstallConfig(cluster *common.Cluster) (*InstallerConfigBaremetal, error) {
	networkType := i.getNetworkType(cluster)
	i.log.Infof("Selected network type %s for cluster %s", networkType, cluster.ID.String())
	cfg := &InstallerConfigBaremetal{
		APIVersion: "v1",
		BaseDomain: cluster.BaseDNSDomain,
		Networking: struct {
			NetworkType    string `yaml:"networkType"`
			ClusterNetwork []struct {
				Cidr       string `yaml:"cidr"`
				HostPrefix int    `yaml:"hostPrefix"`
			} `yaml:"clusterNetwork"`
			MachineNetwork []struct {
				Cidr string `yaml:"cidr"`
			} `yaml:"machineNetwork,omitempty"`
			ServiceNetwork []string `yaml:"serviceNetwork"`
		}{
			NetworkType: networkType,
			ClusterNetwork: []struct {
				Cidr       string `yaml:"cidr"`
				HostPrefix int    `yaml:"hostPrefix"`
			}{
				{Cidr: cluster.ClusterNetworkCidr, HostPrefix: int(cluster.ClusterNetworkHostPrefix)},
			},
			MachineNetwork: []struct {
				Cidr string `yaml:"cidr"`
			}{
				{Cidr: cluster.MachineNetworkCidr},
			},
			ServiceNetwork: []string{cluster.ServiceNetworkCidr},
		},
		Metadata: struct {
			Name string `yaml:"name"`
		}{
			Name: cluster.Name,
		},
		Compute: []struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			{
				Hyperthreading: i.getHypethreadingConfiguration(cluster, "worker"),
				Name:           string(models.HostRoleWorker),
				Replicas:       i.countHostsByRole(cluster, models.HostRoleWorker),
			},
		},
		ControlPlane: struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			Hyperthreading: i.getHypethreadingConfiguration(cluster, "master"),
			Name:           string(models.HostRoleMaster),
			Replicas:       i.countHostsByRole(cluster, models.HostRoleMaster),
		},
		PullSecret: cluster.PullSecret,
		SSHKey:     cluster.SSHPublicKey,
	}

	if cluster.HTTPProxy != "" || cluster.HTTPSProxy != "" {
		cfg.Proxy = &proxy{
			HTTPProxy:  cluster.HTTPProxy,
			HTTPSProxy: cluster.HTTPSProxy,
			NoProxy:    i.generateNoProxy(cluster),
		}
	}

	if i.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		err := i.setImageContentSources(cfg)
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func (i *installConfigBuilder) setImageContentSources(cfg *InstallerConfigBaremetal) error {
	mirrorRegistriesConfigs, err := i.mirrorRegistriesBuilder.ExtractLocationMirrorDataFromRegistries()
	if err != nil {
		i.log.WithError(err).Errorf("Failed to get the mirror registries conf need for ImageContentSources")
		return err
	}
	imageContentSourceList := make([]imageContentSource, len(mirrorRegistriesConfigs))
	for i, mirrorRegistriesConfig := range mirrorRegistriesConfigs {
		imageContentSourceList[i] = imageContentSource{Source: mirrorRegistriesConfig.Location, Mirrors: []string{mirrorRegistriesConfig.Mirror}}
	}
	cfg.ImageContentSources = imageContentSourceList
	return nil
}

func (i *installConfigBuilder) setBMPlatformInstallconfig(cluster *common.Cluster, cfg *InstallerConfigBaremetal) error {
	// set hosts
	numMasters := i.countHostsByRole(cluster, models.HostRoleMaster)
	numWorkers := i.countHostsByRole(cluster, models.HostRoleWorker)
	hosts := make([]host, numWorkers+numMasters)

	yamlHostIdx := 0
	sortedHosts := make([]*models.Host, len(cluster.Hosts))
	copy(sortedHosts, cluster.Hosts)
	sort.Slice(sortedHosts, func(i, j int) bool {
		// sort logic: masters before workers, between themselves - by hostname
		if sortedHosts[i].Role != sortedHosts[j].Role {
			return sortedHosts[i].Role == models.HostRoleMaster
		}
		return hostutil.GetHostnameForMsg(sortedHosts[i]) < hostutil.GetHostnameForMsg(sortedHosts[j])
	})
	for _, host := range sortedHosts {
		if swag.StringValue(host.Status) == models.HostStatusDisabled {
			continue
		}
		hostName := hostutil.GetHostnameForMsg(host)
		i.log.Infof("host name is %s", hostName)
		hosts[yamlHostIdx].Name = hostName
		hosts[yamlHostIdx].Role = string(host.Role)

		var inventory models.Inventory
		err := json.Unmarshal([]byte(host.Inventory), &inventory)
		if err != nil {
			i.log.Warnf("Failed to unmarshall host %s inventory", hostutil.GetHostnameForMsg(host))
			return err
		}
		hosts[yamlHostIdx].BootMACAddress = inventory.Interfaces[0].MacAddress
		hosts[yamlHostIdx].BootMode = "UEFI"
		if inventory.Boot != nil && inventory.Boot.CurrentBootMode != "uefi" {
			hosts[yamlHostIdx].BootMode = "legacy"
		}
		yamlHostIdx += 1
	}

	enableMetal3Provisioning, err := common.VersionGreaterOrEqual(cluster.Cluster.OpenshiftVersion, "4.7")
	if err != nil {
		return err
	}
	provNetwork := "Unmanaged"
	if enableMetal3Provisioning {
		provNetwork = "Disabled"
	}
	i.log.Infof("setting Baremetal.ProvisioningNetwork to %s", provNetwork)

	cfg.Platform = platform{
		Baremetal: &baremetal{
			ProvisioningNetwork: provNetwork,
			APIVIP:              cluster.APIVip,
			IngressVIP:          cluster.IngressVip,
			Hosts:               hosts,
		},
		None: nil,
	}
	return nil
}

func (i *installConfigBuilder) applyConfigOverrides(overrides string, cfg *InstallerConfigBaremetal) error {
	if overrides == "" {
		return nil
	}

	if err := json.Unmarshal([]byte(overrides), cfg); err != nil {
		return err
	}
	return nil
}

func (i *installConfigBuilder) getInstallConfig(cluster *common.Cluster, addRhCa bool, ca string) (*InstallerConfigBaremetal, error) {
	cfg, err := i.getBasicInstallConfig(cluster)
	if err != nil {
		return nil, err
	}

	if swag.BoolValue(cluster.UserManagedNetworking) {
		cfg.Platform = platform{
			Baremetal: nil,
			None:      &platformNone{},
		}

		bootstrapCidr := network.GetMachineCidrForUserManagedNetwork(cluster, i.log)
		if bootstrapCidr != "" {
			i.log.Infof("None-Platform: Selected bootstrap machine network CIDR %s for cluster %s", bootstrapCidr, cluster.ID.String())
			cfg.Networking.MachineNetwork = []struct {
				Cidr string `yaml:"cidr"`
			}{
				{Cidr: bootstrapCidr},
			}
			cluster.MachineNetworkCidr = bootstrapCidr
			cfg.Networking.NetworkType = i.getNetworkType(cluster)

		} else {
			cfg.Networking.MachineNetwork = nil
		}

		if common.IsSingleNodeCluster(cluster) {
			bootstrap := common.GetBootstrapHost(cluster)
			if bootstrap != nil {
				cfg.BootstrapInPlace = bootstrapInPlace{InstallationDisk: hostutil.GetHostInstallationPath(bootstrap)}
			}
		}

	} else {
		err = i.setBMPlatformInstallconfig(cluster, cfg)
		if err != nil {
			return nil, err
		}
	}

	err = i.applyConfigOverrides(cluster.InstallConfigOverrides, cfg)
	if err != nil {
		return nil, err
	}
	caContent := i.getCAContents(cluster, ca, addRhCa)
	if caContent != "" {
		cfg.AdditionalTrustBundle = fmt.Sprintf(` | %s`, caContent)
	}

	return cfg, nil
}

func (i *installConfigBuilder) GetInstallConfig(cluster *common.Cluster, addRhCa bool, ca string) ([]byte, error) {
	cfg, err := i.getInstallConfig(cluster, addRhCa, ca)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(*cfg)
}

func (i *installConfigBuilder) ValidateInstallConfigPatch(cluster *common.Cluster, patch string) error {
	config, err := i.getInstallConfig(cluster, false, "")
	if err != nil {
		return err
	}

	err = i.applyConfigOverrides(patch, config)
	if err != nil {
		return err
	}

	return config.Validate()
}

func (i *installConfigBuilder) getHypethreadingConfiguration(cluster *common.Cluster, machineType string) string {
	switch cluster.Hyperthreading {
	case models.ClusterHyperthreadingAll:
		return "Enabled"
	case models.ClusterHyperthreadingMasters:
		if machineType == "master" {
			return "Enabled"
		}
	case models.ClusterHyperthreadingWorkers:
		if machineType == "worker" {
			return "Enabled"
		}
	}
	return "Disabled"
}

func (i *installConfigBuilder) getCAContents(cluster *common.Cluster, rhRootCA string, installRHRootCAFlag bool) string {
	// CA for mirror registries and RH CA are mutually exclusive
	if i.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		caContents, err := i.mirrorRegistriesBuilder.GetMirrorCA()
		if err == nil {
			return "\n" + string(caContents)
		}
	}
	if installRHRootCAFlag {
		return rhRootCA
	}
	return ""
}
