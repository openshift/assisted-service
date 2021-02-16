package installcfg

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
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
	Baremetal *baremetal    `yaml:"baremetal,omitempty"`
	None      *platformNone `yaml:"none,omitempty"`
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
		Hyperthreading string `yaml:"hyperthreading"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"compute"`
	ControlPlane struct {
		Hyperthreading string `yaml:"hyperthreading"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"controlPlane"`
	Platform              platform         `yaml:"platform"`
	BootstrapInPlace      bootstrapInPlace `yaml:"bootstrapInPlace"`
	FIPS                  bool             `yaml:"fips"`
	PullSecret            string           `yaml:"pullSecret"`
	SSHKey                string           `yaml:"sshKey"`
	AdditionalTrustBundle string           `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []struct {
		Mirrors []string `yaml:"mirrors"`
		Source  string   `yaml:"source"`
	} `yaml:"imageContentSources,omitempty"`
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

func countHostsByRole(cluster *common.Cluster, role models.HostRole) int {
	var count int
	for _, host := range cluster.Hosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled && host.Role == role {
			count += 1
		}
	}
	return count
}

func getBMHName(host *models.Host, masterIdx, workerIdx *int) string {
	prefix := "openshift-master-"
	index := masterIdx
	if host.Role == models.HostRoleWorker {
		prefix = "openshift-worker-"
		index = workerIdx
	}
	name := prefix + strconv.Itoa(*index)
	*index = *index + 1
	return name
}

func getNetworkType(cluster *common.Cluster) string {
	networkType := "OpenShiftSDN"
	if network.IsIPv6CIDR(cluster.ClusterNetworkCidr) || network.IsIPv6CIDR(cluster.MachineNetworkCidr) || network.IsIPv6CIDR(cluster.ServiceNetworkCidr) {
		networkType = "OVNKubernetes"
	}
	return networkType
}

func generateNoProxy(cluster *common.Cluster) string {
	noProxy := strings.TrimSpace(cluster.NoProxy)
	splitNoProxy := funk.FilterString(strings.Split(noProxy, ","), func(s string) bool { return s != "" })
	if cluster.MachineNetworkCidr != "" {
		splitNoProxy = append(splitNoProxy, cluster.MachineNetworkCidr)
	}
	// Add internal OCP DNS domain
	internalDnsDomain := "." + cluster.Name + "." + cluster.BaseDNSDomain
	return strings.Join(append(splitNoProxy, internalDnsDomain, cluster.ClusterNetworkCidr, cluster.ServiceNetworkCidr), ",")
}

func getBasicInstallConfig(log logrus.FieldLogger, cluster *common.Cluster) *InstallerConfigBaremetal {
	networkType := getNetworkType(cluster)
	log.Infof("Selected network type %s for cluster %s", networkType, cluster.ID.String())
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
			Hyperthreading string `yaml:"hyperthreading"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			{
				Hyperthreading: "Enabled",
				Name:           string(models.HostRoleWorker),
				Replicas:       countHostsByRole(cluster, models.HostRoleWorker),
			},
		},
		ControlPlane: struct {
			Hyperthreading string `yaml:"hyperthreading"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			Hyperthreading: "Enabled",
			Name:           string(models.HostRoleMaster),
			Replicas:       countHostsByRole(cluster, models.HostRoleMaster),
		},
		PullSecret: cluster.PullSecret,
		SSHKey:     cluster.SSHPublicKey,
	}

	if cluster.HTTPProxy != "" || cluster.HTTPSProxy != "" {
		cfg.Proxy = &proxy{
			HTTPProxy:  cluster.HTTPProxy,
			HTTPSProxy: cluster.HTTPSProxy,
			NoProxy:    generateNoProxy(cluster),
		}
	}
	return cfg
}

func setBMPlatformInstallconfig(log logrus.FieldLogger, cluster *common.Cluster, cfg *InstallerConfigBaremetal) error {
	// set hosts
	numMasters := countHostsByRole(cluster, models.HostRoleMaster)
	numWorkers := countHostsByRole(cluster, models.HostRoleWorker)
	hosts := make([]host, numWorkers+numMasters)

	yamlHostIdx := 0
	masterIdx := 0
	workerIdx := 0
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
		log.Infof("host name is %s", hostutil.GetHostnameForMsg(host))
		hosts[yamlHostIdx].Name = getBMHName(host, &masterIdx, &workerIdx)
		hosts[yamlHostIdx].Role = string(host.Role)

		var inventory models.Inventory
		err := json.Unmarshal([]byte(host.Inventory), &inventory)
		if err != nil {
			log.Warnf("Failed to unmarshall host %s inventory", hostutil.GetHostnameForMsg(host))
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
	log.Infof("setting Baremetal.ProvisioningNetwork to %s", provNetwork)

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

func applyConfigOverrides(overrides string, cfg *InstallerConfigBaremetal) error {
	if overrides == "" {
		return nil
	}

	if err := json.Unmarshal([]byte(overrides), cfg); err != nil {
		return err
	}
	return nil
}

func getInstallConfig(log logrus.FieldLogger, cluster *common.Cluster, addRhCa bool, ca string) (*InstallerConfigBaremetal, error) {
	cfg := getBasicInstallConfig(log, cluster)
	if swag.BoolValue(cluster.UserManagedNetworking) {
		cfg.Platform = platform{
			Baremetal: nil,
			None:      &platformNone{},
		}

		_, bootstrapCidr := common.GetBootstrapMachineNetworkAndIp(cluster)
		if bootstrapCidr != "" {
			log.Infof("None-Platform: Selected bootstrap machine network CIDR %s for cluster %s", bootstrapCidr, cluster.ID.String())
			cfg.Networking.MachineNetwork = []struct {
				Cidr string `yaml:"cidr"`
			}{
				{Cidr: bootstrapCidr},
			}
		} else {
			cfg.Networking.MachineNetwork = nil
		}

		if common.IsSingleNodeCluster(cluster) {
			cfg.BootstrapInPlace = bootstrapInPlace{InstallationDisk: common.GetBootstrapHost(cluster).InstallationDiskPath}
		}

	} else {
		err := setBMPlatformInstallconfig(log, cluster, cfg)
		if err != nil {
			return nil, err
		}
	}

	err := applyConfigOverrides(cluster.InstallConfigOverrides, cfg)
	if err != nil {
		return nil, err
	}
	if addRhCa {
		cfg.AdditionalTrustBundle = fmt.Sprintf(` | %s`, ca)
	}

	return cfg, nil
}

func GetInstallConfig(log logrus.FieldLogger, cluster *common.Cluster, addRhCa bool, ca string) ([]byte, error) {
	cfg, err := getInstallConfig(log, cluster, addRhCa, ca)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(*cfg)
}

func ValidateInstallConfigPatch(log logrus.FieldLogger, cluster *common.Cluster, patch string) error {
	config, err := getInstallConfig(log, cluster, false, "")
	if err != nil {
		return err
	}

	err = applyConfigOverrides(patch, config)
	if err != nil {
		return err
	}

	return config.Validate()
}
