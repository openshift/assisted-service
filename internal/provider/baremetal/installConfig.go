package baremetal

import (
	"encoding/json"
	"sort"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

func (p baremetalProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	// set hosts
	numMasters := cfg.ControlPlane.Replicas
	// TODO: will we always have just one compute?
	numWorkers := cfg.Compute[0].Replicas
	hosts := make([]installcfg.Host, numWorkers+numMasters)

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
		hostName := hostutil.GetHostnameForMsg(host)
		p.Log.Infof("Host name is %s", hostName)
		hosts[yamlHostIdx].Name = hostName
		hosts[yamlHostIdx].Role = string(host.Role)

		var inventory models.Inventory
		err := json.Unmarshal([]byte(host.Inventory), &inventory)
		if err != nil {
			p.Log.Warnf("Failed to unmarshall Host %s inventory", hostutil.GetHostnameForMsg(host))
			return err
		}

		for _, iface := range inventory.Interfaces {
			if iface.MacAddress != "" {
				hosts[yamlHostIdx].BootMACAddress = iface.MacAddress
				break
			}
		}
		if hosts[yamlHostIdx].BootMACAddress == "" {
			err = errors.Errorf("Failed to find an interface with a mac address for host %s", hostutil.GetHostnameForMsg(host))
			p.Log.Error(err)
			return err
		}

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
	p.Log.Infof("setting Baremetal.ProvisioningNetwork to %s", provNetwork)

	cfg.Platform = installcfg.Platform{
		Baremetal: &installcfg.BareMetalInstallConfigPlatform{
			ProvisioningNetwork: provNetwork,
			APIVIP:              cluster.APIVip,
			IngressVIP:          cluster.IngressVip,
			Hosts:               hosts,
		},
	}
	return nil
}
