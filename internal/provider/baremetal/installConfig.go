package baremetal

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

func (p *baremetalProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster, infraEnvs []*common.InfraEnv) error {
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

		if len(cluster.MachineNetworks) == 0 {
			err = errors.Errorf("Failed to find machine networks for baremetal cluster %s", cluster.ID)
			p.Log.Error(err)
			return err
		}

	InterfaceSearch:
		for _, iface := range inventory.Interfaces {
			// We are looking for the NIC that matches the first Machine Network configured
			// for the cluster. This is to ensure that BootMACAddress belongs to the NIC that
			// is really used and not to any fake interface even if this interface has IPs.
			ipAddressses := append(iface.IPV4Addresses, iface.IPV6Addresses...)
			for _, ip := range ipAddressses {
				for _, machineNetwork := range cluster.MachineNetworks {
					overlap, _ := network.NetworksOverlap(ip, string(machineNetwork.Cidr))
					if overlap {
						hosts[yamlHostIdx].BootMACAddress = iface.MacAddress
						break InterfaceSearch
					}
				}
			}
		}
		if hosts[yamlHostIdx].BootMACAddress == "" {
			err = errors.Errorf("Failed to find a network interface matching machine networks for host %s",
				hostutil.GetHostnameForMsg(host),
			)
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

	if featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDDUALSTACKVIPS, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture)) {
		cfg.Platform = installcfg.Platform{
			Baremetal: &installcfg.BareMetalInstallConfigPlatform{
				ProvisioningNetwork: provNetwork,
				APIVIPs:             network.GetApiVips(cluster),
				IngressVIPs:         network.GetIngressVips(cluster),
				Hosts:               hosts,
			},
		}
	} else {
		cfg.Platform = installcfg.Platform{
			Baremetal: &installcfg.BareMetalInstallConfigPlatform{
				ProvisioningNetwork:  provNetwork,
				APIVIPs:              []string{network.GetApiVips(cluster)[0]},
				IngressVIPs:          []string{network.GetIngressVips(cluster)[0]},
				DeprecatedAPIVIP:     network.GetApiVipById(cluster, 0),
				DeprecatedIngressVIP: network.GetIngressVipById(cluster, 0),
				Hosts:                hosts,
			},
		}
	}

	// We want to use the NTP sources specified in the cluster, and if that is empty, the ones specified in the
	// infrastructure environment. Note that in some rare cases there may be multiple infrastructure environments,
	// so we add the NTP sources of all of them.
	ntpServers := p.splitNTPSources(cluster.AdditionalNtpSource)
	if len(ntpServers) == 0 {
		for _, infraEnv := range infraEnvs {
			for _, ntpSource := range p.splitNTPSources(infraEnv.AdditionalNtpSources) {
				if !slices.Contains(ntpServers, ntpSource) {
					ntpServers = append(ntpServers, ntpSource)
				}
			}
		}
	}

	// Note that the new `additionalNTPServers` field was added in OpenShift 4.18, but we add it to all versions
	// here because older versions will just ignore it.
	cfg.Platform.Baremetal.AdditionalNTPServers = ntpServers

	return nil
}

func (p *baremetalProvider) splitNTPSources(sources string) []string {
	split := strings.Split(sources, ",")
	var result []string
	for _, source := range split {
		source = strings.TrimSpace(source)
		if source != "" {
			result = append(result, source)
		}
	}
	return result
}
