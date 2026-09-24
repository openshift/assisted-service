package constants

// NetworkSysctlConfig is applied on discovery hosts to reduce rx_dropped during
// live rootfs downloads over PXE/virtual media.
const NetworkSysctlConfig = `# Increase network receive buffers for assisted installation boot
net.core.netdev_max_backlog = 2000
net.core.rmem_max = 16777216
`

const NetworkSysctlTuningScript = `#!/bin/bash
set -euo pipefail
sysctl -w net.core.netdev_max_backlog=2000
sysctl -w net.core.rmem_max=16777216
`

const NetworkSysctlTuningService = `[Unit]
Description=Apply network sysctl tuning for assisted installation
DefaultDependencies=no
Before=coreos-livepxe-rootfs.service
After=systemd-sysctl.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/assisted-network-sysctl.sh
`
