package installcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/filanov/bm-inventory/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type InstallerConfigNone struct {
	APIVersion string `yaml:"apiVersion"`
	BaseDomain string `yaml:"baseDomain"`
	Compute    []struct {
		Hyperthreading string `yaml:"hyperthreading"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"compute"`
	ControlPlane struct {
		Hyperthreading string `yaml:"hyperthreading"`
		Name           string `yaml:"name"`
		Replicas       int    `yaml:"replicas"`
	} `yaml:"controlPlane"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Networking struct {
		ClusterNetwork []struct {
			Cidr       string `yaml:"cidr"`
			HostPrefix int    `yaml:"hostPrefix"`
		} `yaml:"clusterNetwork"`
		NetworkType    string   `yaml:"networkType"`
		ServiceNetwork []string `yaml:"serviceNetwork"`
	} `yaml:"networking"`
	Platform struct {
		None struct {
		} `yaml:"none"`
	} `yaml:"platform"`
	PullSecret string `yaml:"pullSecret"`
	SSHKey     string `yaml:"sshKey"`
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
	Platform struct {
		Baremetal struct {
			ProvisioningNetworkInterface string `yaml:"provisioningNetworkInterface"`
			APIVIP                       string `yaml:"apiVIP"`
			IngressVIP                   string `yaml:"ingressVIP"`
			DNSVIP                       string `yaml:"dnsVIP"`
			Hosts                        []struct {
				Name string `yaml:"name"`
				Role string `yaml:"role"`
				Bmc  struct {
					Address  string `yaml:"address"`
					Username string `yaml:"username"`
					Password string `yaml:"password"`
				} `yaml:"bmc"`
				BootMACAddress  string `yaml:"bootMACAddress"`
				BootMode        string `yaml:"bootMode"`
				HardwareProfile string `yaml:"hardwareProfile"`
			} `yaml:"hosts"`
		} `yaml:"baremetal"`
	} `yaml:"platform"`
	PullSecret string `yaml:"pullSecret"`
	SSHKey     string `yaml:"sshKey"`
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

func getMachineCIDR(cluster *models.Cluster) (string, error) {
	parsedVipAddr := net.ParseIP(string(cluster.APIVip))
	if parsedVipAddr == nil {
		errStr := fmt.Sprintf("Could not parse VIP ip %s", cluster.APIVip)
		logrus.Warn(errStr)
		return "", errors.New(errStr)
	}
	for _, h := range cluster.Hosts {
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			logrus.WithError(err).Warnf("Error unmarshalling host inventory %s", h.Inventory)
			continue
		}
		for _, intf := range inventory.Interfaces {
			for _, ipv4addr := range intf.IPV4Addresses {
				_, ipnet, err := net.ParseCIDR(ipv4addr)
				if err != nil {
					logrus.WithError(err).Warnf("Could not parse cidr %s", ipv4addr)
					continue
				}
				if ipnet.Contains(parsedVipAddr) {
					return ipnet.String(), nil
				}
			}
		}
	}
	errStr := fmt.Sprintf("No suitable matching CIDR found for VIP %s", cluster.APIVip)
	logrus.Warn(errStr)
	return "", errors.New(errStr)
}

func GetInstallConfig(cluster *models.Cluster) ([]byte, error) {
	machineCidr, err := getMachineCIDR(cluster)
	if err != nil {
		return nil, err
	}
	var cfg interface{}
	if cluster.OpenshiftVersion != models.ClusterOpenshiftVersionNr44 {
		cfg = InstallerConfigBaremetal{
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
					{Cidr: machineCidr},
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
			Platform: struct {
				Baremetal struct {
					ProvisioningNetworkInterface string `yaml:"provisioningNetworkInterface"`
					APIVIP                       string `yaml:"apiVIP"`
					IngressVIP                   string `yaml:"ingressVIP"`
					DNSVIP                       string `yaml:"dnsVIP"`
					Hosts                        []struct {
						Name string `yaml:"name"`
						Role string `yaml:"role"`
						Bmc  struct {
							Address  string `yaml:"address"`
							Username string `yaml:"username"`
							Password string `yaml:"password"`
						} `yaml:"bmc"`
						BootMACAddress  string `yaml:"bootMACAddress"`
						BootMode        string `yaml:"bootMode"`
						HardwareProfile string `yaml:"hardwareProfile"`
					} `yaml:"hosts"`
				} `yaml:"baremetal"`
			}{
				Baremetal: struct {
					ProvisioningNetworkInterface string `yaml:"provisioningNetworkInterface"`
					APIVIP                       string `yaml:"apiVIP"`
					IngressVIP                   string `yaml:"ingressVIP"`
					DNSVIP                       string `yaml:"dnsVIP"`
					Hosts                        []struct {
						Name string `yaml:"name"`
						Role string `yaml:"role"`
						Bmc  struct {
							Address  string `yaml:"address"`
							Username string `yaml:"username"`
							Password string `yaml:"password"`
						} `yaml:"bmc"`
						BootMACAddress  string `yaml:"bootMACAddress"`
						BootMode        string `yaml:"bootMode"`
						HardwareProfile string `yaml:"hardwareProfile"`
					} `yaml:"hosts"`
				}{
					ProvisioningNetworkInterface: "ethh0",
					APIVIP:                       cluster.APIVip.String(),
					IngressVIP:                   cluster.IngressVip.String(),
					DNSVIP:                       cluster.DNSVip.String(),
					Hosts: []struct {
						Name string `yaml:"name"`
						Role string `yaml:"role"`
						Bmc  struct {
							Address  string `yaml:"address"`
							Username string `yaml:"username"`
							Password string `yaml:"password"`
						} `yaml:"bmc"`
						BootMACAddress  string `yaml:"bootMACAddress"`
						BootMode        string `yaml:"bootMode"`
						HardwareProfile string `yaml:"hardwareProfile"`
					}{
						{
							Name: "openshift-master-0",
							Role: "master",
							Bmc: struct {
								Address  string `yaml:"address"`
								Username string `yaml:"username"`
								Password string `yaml:"password"`
							}{
								Address:  "ipmi://192.168.111.1:6230",
								Username: "admin",
								Password: "rackattack",
							},
							BootMACAddress:  "00:aa:39:b3:51:f4",
							BootMode:        "UEFI",
							HardwareProfile: "unknown",
						},
						{
							Name: "openshift-master-1",
							Role: "master",
							Bmc: struct {
								Address  string `yaml:"address"`
								Username string `yaml:"username"`
								Password string `yaml:"password"`
							}{
								Address:  "ipmi://192.168.111.1:6231",
								Username: "admin",
								Password: "rackattack",
							},
							BootMACAddress:  "00:aa:39:b3:51:f5",
							BootMode:        "UEFI",
							HardwareProfile: "unknown",
						},
						{
							Name: "openshift-master-2",
							Role: "master",
							Bmc: struct {
								Address  string `yaml:"address"`
								Username string `yaml:"username"`
								Password string `yaml:"password"`
							}{
								Address:  "ipmi://192.168.111.1:6232",
								Username: "admin",
								Password: "rackattack",
							},
							BootMACAddress:  "00:aa:39:b3:51:f6",
							BootMode:        "UEFI",
							HardwareProfile: "unknown",
						},
						{
							Name: "openshift-worker-0",
							Role: "worker",
							Bmc: struct {
								Address  string `yaml:"address"`
								Username string `yaml:"username"`
								Password string `yaml:"password"`
							}{
								Address:  "ipmi://192.168.111.1:6233",
								Username: "admin",
								Password: "rackattack",
							},
							BootMACAddress:  "00:aa:39:b3:51:f7",
							BootMode:        "UEFI",
							HardwareProfile: "unknown",
						},
					},
				},
			},
			PullSecret: cluster.PullSecret,
			SSHKey:     cluster.SSHPublicKey,
		}
	} else {
		cfg = InstallerConfigNone{
			APIVersion: "v1",
			BaseDomain: cluster.BaseDNSDomain,
			Compute: []struct {
				Hyperthreading string `yaml:"hyperthreading"`
				Name           string `yaml:"name"`
				Replicas       int    `yaml:"replicas"`
			}{
				{Hyperthreading: "Enabled", Name: "worker", Replicas: countHostsByRole(cluster, "worker")},
			},
			ControlPlane: struct {
				Hyperthreading string `yaml:"hyperthreading"`
				Name           string `yaml:"name"`
				Replicas       int    `yaml:"replicas"`
			}{
				Hyperthreading: "Enabled",
				Name:           "master",
				Replicas:       countHostsByRole(cluster, "master"),
			},
			Metadata: struct {
				Name string `yaml:"name"`
			}{Name: cluster.Name},
			Networking: struct {
				ClusterNetwork []struct {
					Cidr       string `yaml:"cidr"`
					HostPrefix int    `yaml:"hostPrefix"`
				} `yaml:"clusterNetwork"`
				NetworkType    string   `yaml:"networkType"`
				ServiceNetwork []string `yaml:"serviceNetwork"`
			}{
				ClusterNetwork: []struct {
					Cidr       string `yaml:"cidr"`
					HostPrefix int    `yaml:"hostPrefix"`
				}{
					{Cidr: cluster.ClusterNetworkCidr, HostPrefix: int(cluster.ClusterNetworkHostPrefix)},
				},
				NetworkType:    "OpenShiftSDN",
				ServiceNetwork: []string{cluster.ServiceNetworkCidr},
			},
			Platform: struct {
				None struct{} `yaml:"none"`
			}{},
			PullSecret: cluster.PullSecret,
			SSHKey:     cluster.SSHPublicKey,
		}
	}
	return yaml.Marshal(cfg)
}
