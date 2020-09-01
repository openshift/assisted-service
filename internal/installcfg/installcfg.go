package installcfg

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type bmc struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type host struct {
	Name            string `yaml:"name"`
	Role            string `yaml:"role"`
	Bmc             bmc    `yaml:"bmc"`
	BootMACAddress  string `yaml:"bootMACAddress"`
	BootMode        string `yaml:"bootMode"`
	HardwareProfile string `yaml:"hardwareProfile"`
}

type baremetal struct {
	ProvisioningNetworkInterface string `yaml:"provisioningNetworkInterface"`
	APIVIP                       string `yaml:"apiVIP"`
	IngressVIP                   string `yaml:"ingressVIP"`
	DNSVIP                       string `yaml:"dnsVIP"`
	Hosts                        []host `yaml:"hosts"`
}

type platform struct {
	Baremetal baremetal `yaml:"baremetal"`
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
		} `yaml:"machineNetwork"`
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
			} `yaml:"machineNetwork"`
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

// [TODO] - remove once we decide to use specific values from the hosts of the cluster
func getDummyMAC(log logrus.FieldLogger, dummyMAC string, count int) (string, error) {
	hwMac, err := net.ParseMAC(dummyMAC)
	if err != nil {
		log.Warn("Failed to parse dummyMac")
		return "", err
	}
	hwMac[len(hwMac)-1] = hwMac[len(hwMac)-1] + byte(count)
	return hwMac.String(), nil
}

func setBMPlatformInstallconfig(log logrus.FieldLogger, cluster *common.Cluster, cfg *InstallerConfigBaremetal) error {
	// set hosts
	numMasters := countHostsByRole(cluster, models.HostRoleMaster)
	numWorkers := countHostsByRole(cluster, models.HostRoleWorker)
	masterCount := 0
	workerCount := 0
	hosts := make([]host, numWorkers+numMasters)

	// dummy MAC and port, once we start using real BMH, those values should be set from cluster
	dummyMAC := "00:aa:39:b3:51:10"
	dummyPort := 6230

	for i := range hosts {
		log.Infof("Setting master, host %d, master count %d", i, masterCount)
		if i >= numMasters {
			hosts[i].Name = fmt.Sprintf("openshift-worker-%d", workerCount)
			hosts[i].Role = string(models.HostRoleWorker)
			workerCount += 1
		} else {
			hosts[i].Name = fmt.Sprintf("openshift-master-%d", masterCount)
			hosts[i].Role = string(models.HostRoleMaster)
			masterCount += 1
		}
		hosts[i].Bmc = bmc{
			Address:  fmt.Sprintf("ipmi://192.168.111.1:%d", dummyPort+i),
			Username: "admin",
			Password: "rackattack",
		}
		hwMac, err := getDummyMAC(log, dummyMAC, i)
		if err != nil {
			log.Warn("Failed to parse dummyMac")
			return err
		}
		hosts[i].BootMACAddress = hwMac
		hosts[i].BootMode = "UEFI"
		hosts[i].HardwareProfile = "unknown"
	}
	cfg.Platform = platform{
		Baremetal: baremetal{
			ProvisioningNetworkInterface: "ens4",
			APIVIP:                       cluster.APIVip,
			IngressVIP:                   cluster.IngressVip,
			DNSVIP:                       cluster.APIVip,
			Hosts:                        hosts,
		},
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

func GetInstallConfig(log logrus.FieldLogger, cluster *common.Cluster) ([]byte, error) {
	cfg := getBasicInstallConfig(cluster)
	err := setBMPlatformInstallconfig(log, cluster, cfg)
	if err != nil {
		return nil, err
	}

	err = applyConfigOverrides(cluster.InstallConfigOverrides, cfg)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(*cfg)
}

func ValidateInstallConfigJSON(s string) error {
	return json.Unmarshal([]byte(s), &InstallerConfigBaremetal{})
}
