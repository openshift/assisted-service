package common

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
)

const (
	EnvConfigPrefix = "myapp"

	MinMasterHostsNeededForInstallation    = 3
	AllowedNumberOfMasterHostsInNoneHaMode = 1
	AllowedNumberOfWorkersInNoneHaMode     = 0
	IllegalWorkerHostsCount                = 1

	HostCACertPath = "/etc/assisted-service/service-ca-cert.crt"

	consoleUrlPrefix = "https://console-openshift-console.apps"

	MirrorRegistriesCertificateFile = "tls-ca-bundle.pem"
	MirrorRegistriesCertificatePath = "/etc/pki/ca-trust/extracted/pem/" + MirrorRegistriesCertificateFile
	MirrorRegistriesConfigDir       = "/etc/containers"
	MirrorRegistriesConfigFile      = "registries.conf"
	MirrorRegistriesConfigPath      = MirrorRegistriesConfigDir + "/" + MirrorRegistriesConfigFile
	MaximumAllowedTimeDiffMinutes   = 4

	FamilyIPv4 int32 = 4
	FamilyIPv6 int32 = 6
)

// Configuration to be injected by discovery ignition.  It will cause IPv6 DHCP client identifier to be the same
// after reboot.  This will cause the DHCP server to provide the same IP address after reboot.
const Ipv6DuidDiscoveryConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
`

// Configuration to be used by MCO manifest to get consistent IPv6 DHCP client identification.
const Ipv6DuidRuntimeConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
[keyfile]
path=/etc/NetworkManager/system-connections-merged
`

// configuration of NM to disable handling of /etc/resolv.conf
// used for configuration of bootstrap node during bootkube (before reboot)
// and of masters after reboot
const UnmanagedResolvConf = `
[main]
rc-manager=unmanaged
`

// NM configuration to be activated (set into discovery ignition) in case we want more logging for NM debugging purposes.
// This content needs to be set in the /etc/NetworkManager/conf.d/95-nm-debug.conf
// In addition, the line RateLimitBurst=0 must be uncommented in the /etc/systemd/journald.conf and systemctl restart systemd-journald run.
const NMDebugModeConf = `
[logging]
domains=ALL:DEBUG
`

func AllStrings(vs []string, f func(string) bool) bool {
	for _, v := range vs {
		if !f(v) {
			return false
		}
	}
	return true
}

// GetBootstrapHost return host that was set as bootstrap
func GetBootstrapHost(cluster *Cluster) *models.Host {
	for _, host := range cluster.Hosts {
		if host.Bootstrap {
			return host
		}
	}
	return nil
}

// IsSingleNodeCluster if this cluster is single-node or not
func IsSingleNodeCluster(cluster *Cluster) bool {
	return swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
}

func GetConsoleUrl(clusterName, baseDomain string) string {
	return fmt.Sprintf("%s.%s.%s", consoleUrlPrefix, clusterName, baseDomain)
}

func IsNtpSynced(c *Cluster) (bool, error) {
	var min int64
	var max int64
	for _, h := range c.Hosts {
		if h.Inventory == "" || *h.Status == models.HostStatusDisconnected ||
			*h.Status == models.HostStatusDisabled || *h.Status == models.HostStatusResettingPendingUserAction ||
			*h.Status == models.HostStatusDiscovering {
			continue
		}
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			return false, err
		}

		if inventory.Timestamp < min || min == 0 {
			min = inventory.Timestamp
		}
		if inventory.Timestamp > max {
			max = inventory.Timestamp
		}
	}
	return (max-min)/60 <= MaximumAllowedTimeDiffMinutes, nil
}
