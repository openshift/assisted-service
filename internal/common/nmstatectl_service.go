package common

import (
	"bytes"
	"text/template"
)

const minimalISONetworkConfigServiceNmstatectlFormat = `
[Unit]
Description=Assisted static network config
DefaultDependencies=no
After=nm-initrd.service systemd-udev-settle.service
Before=coreos-livepxe-rootfs.service

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=DISCOVERY_DELAY_SECONDS={{.DiscoveryDelaySeconds}}
TimeoutSec={{.TimeoutSec}}
ExecStart=/usr/local/bin/pre-network-manager-config.sh
`

// FormatMinimalISONetworkConfigServiceNmstatectl returns the minimal ISO systemd unit for the nmstatectl path.
func FormatMinimalISONetworkConfigServiceNmstatectl(discoveryDelaySeconds int64) (string, error) {
	params := map[string]int64{
		"DiscoveryDelaySeconds": discoveryDelaySeconds,
		"TimeoutSec":            60 + discoveryDelaySeconds,
	}
	tmpl, err := template.New("minimalISONetworkConfigServiceNmstatectl").Parse(minimalISONetworkConfigServiceNmstatectlFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, params); err != nil {
		return "", err
	}
	return buf.String(), nil
}
