package installcfg

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hostutil"
	"github.com/openshift/assisted-service/models"

	"github.com/sirupsen/logrus"
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
	Platform              platform `yaml:"platform"`
	FIPS                  bool     `yaml:"fips"`
	PullSecret            string   `yaml:"pullSecret"`
	SSHKey                string   `yaml:"sshKey"`
	AdditionalTrustBundle string   `yaml:"additionalTrustBundle,omitempty"`
	ImageContentSources   []struct {
		Mirrors []string `yaml:"mirrors"`
		Source  string   `yaml:"source"`
	} `yaml:"imageContentSources,omitempty"`
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

func getBasicInstallConfig(cluster *common.Cluster) *InstallerConfigBaremetal {
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
			NetworkType: "OpenShiftSDN",
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
			NoProxy:    cluster.NoProxy,
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
	cfg.Platform = platform{
		Baremetal: &baremetal{
			ProvisioningNetwork: "Unmanaged",
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

func GetInstallConfig(log logrus.FieldLogger, cluster *common.Cluster, addRhCa bool, ca string) ([]byte, error) {
	cfg := getBasicInstallConfig(cluster)
	if swag.BoolValue(cluster.UserManagedNetworking) {
		cfg.Platform = platform{
			Baremetal: nil,
			None:      &platformNone{},
		}
		cfg.Networking.MachineNetwork = nil

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

	return yaml.Marshal(*cfg)
}

func ValidateInstallConfigJSON(s string) error {
	return json.Unmarshal([]byte(s), &InstallerConfigBaremetal{})
}
