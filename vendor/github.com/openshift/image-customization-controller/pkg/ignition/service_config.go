package ignition

import (
	"fmt"
	"strings"

	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	"k8s.io/utils/pointer"
)

func (b *ignitionBuilder) IronicAgentConf() ignition_config_types_32.File {
	template := `
[DEFAULT]
api_url = %s:6385
inspection_callback_url = %s:5050/v1/continue
insecure = True
enable_vlan_interfaces = %s
`
	contents := fmt.Sprintf(template, b.ironicBaseURL, b.ironicInspectorBaseURL, ironicInspectorVlanInterfaces)
	return ignitionFileEmbed("/etc/ironic-python-agent.conf", 0644, false, []byte(contents))
}

func (b *ignitionBuilder) IronicAgentService(copyNetwork bool) ignition_config_types_32.Unit {
	flags := ironicAgentPodmanFlags
	if b.ironicAgentPullSecret != "" {
		flags += " --authfile=/etc/authfile.json"
	}

	unitTemplate := `[Unit]
Description=Ironic Agent
After=network-online.target
Wants=network-online.target
[Service]
Environment="HTTP_PROXY=%s"
Environment="HTTPS_PROXY=%s"
Environment="NO_PROXY=%s"
TimeoutStartSec=0
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=0
ExecStartPre=/bin/podman pull %s %s
ExecStart=/bin/podman run --rm --privileged --network host --mount type=bind,src=/etc/ironic-python-agent.conf,dst=/etc/ironic-python-agent/ignition.conf --mount type=bind,src=/dev,dst=/dev --mount type=bind,src=/sys,dst=/sys --mount type=bind,src=/run/dbus/system_bus_socket,dst=/run/dbus/system_bus_socket --mount type=bind,src=/,dst=/mnt/coreos --mount type=bind,src=/run/udev,dst=/run/udev --ipc=host --uts=host --env "IPA_COREOS_IP_OPTIONS=%s" --env IPA_COREOS_COPY_NETWORK=%v --env "IPA_DEFAULT_HOSTNAME=%s" --name ironic-agent %s
[Install]
WantedBy=multi-user.target
`
	contents := fmt.Sprintf(unitTemplate, b.httpProxy, b.httpsProxy, b.noProxy, b.ironicAgentImage, flags, b.ipOptions, copyNetwork, b.hostname, b.ironicAgentImage)

	return ignition_config_types_32.Unit{
		Name:     "ironic-agent.service",
		Enabled:  pointer.Bool(true),
		Contents: &contents,
	}
}

func (b *ignitionBuilder) authFile() ignition_config_types_32.File {
	source := "data:;base64," + strings.TrimSpace(b.ironicAgentPullSecret)
	return ignition_config_types_32.File{
		Node:          ignition_config_types_32.Node{Path: "/etc/authfile.json"},
		FileEmbedded1: ignition_config_types_32.FileEmbedded1{Contents: ignition_config_types_32.Resource{Source: &source}},
	}
}
