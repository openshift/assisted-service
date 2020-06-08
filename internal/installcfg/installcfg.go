package installcfg

import (
	"fmt"
	"net"

	"github.com/filanov/bm-inventory/models"
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

type InstallerConfigBaremetal struct {
	APIVersion string `yaml:"apiVersion"`
	BaseDomain string `yaml:"baseDomain"`
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
		Name     string `yaml:"name"`
		Replicas int    `yaml:"replicas"`
	} `yaml:"compute"`
	ControlPlane struct {
		Name     string `yaml:"name"`
		Replicas int    `yaml:"replicas"`
	} `yaml:"controlPlane"`
	Platform   platform `yaml:"platform"`
	PullSecret string   `yaml:"pullSecret"`
	SSHKey     string   `yaml:"sshKey"`
}

func countHostsByRole(cluster *models.Cluster, role string) int {
	var count int
	for _, host := range cluster.Hosts {
		if host.Role == role {
			count += 1
		}
	}
	return count
}

func getBasicInstallConfig(cluster *models.Cluster) *InstallerConfigBaremetal {
	return &InstallerConfigBaremetal{
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
			Name     string `yaml:"name"`
			Replicas int    `yaml:"replicas"`
		}{
			{Name: "worker", Replicas: countHostsByRole(cluster, "worker")},
		},
		ControlPlane: struct {
			Name     string `yaml:"name"`
			Replicas int    `yaml:"replicas"`
		}{
			Name:     "master",
			Replicas: countHostsByRole(cluster, "master"),
		},
		PullSecret: cluster.PullSecret,
		SSHKey:     cluster.SSHPublicKey,
	}
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

func setBMPlatformInstallconfig(log logrus.FieldLogger, cluster *models.Cluster, cfg *InstallerConfigBaremetal) error {
	// set hosts
	numMasters := countHostsByRole(cluster, "master")
	numWorkers := countHostsByRole(cluster, "worker")
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
			hosts[i].Role = "worker"
			workerCount += 1
		} else {
			hosts[i].Name = fmt.Sprintf("openshift-master-%d", masterCount)
			hosts[i].Role = "master"
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
			ProvisioningNetworkInterface: "ethh0",
			APIVIP:                       cluster.APIVip,
			IngressVIP:                   cluster.IngressVip,
			DNSVIP:                       cluster.APIVip,
			Hosts:                        hosts,
		},
	}
	return nil
}

func GetInstallConfig(log logrus.FieldLogger, cluster *models.Cluster) ([]byte, error) {
	cfg := getBasicInstallConfig(cluster)
	err := setBMPlatformInstallconfig(log, cluster, cfg)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(*cfg)
}
