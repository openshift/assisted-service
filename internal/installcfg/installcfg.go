package installcfg

import (
	"github.com/filanov/bm-inventory/models"
	"gopkg.in/yaml.v2"
)

type InstallerConfig struct {
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

func countHostsByRole(cluster *models.Cluster, role string) int {
	var count int
	for _, host := range cluster.Hosts {
		if host.Role == role {
			count += 1
		}
	}
	return count
}

func GetInstallConfig(cluster *models.Cluster) ([]byte, error) {
	cfg := InstallerConfig{
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

	return yaml.Marshal(cfg)
}
