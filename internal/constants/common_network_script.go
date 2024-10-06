package constants

const CommonNetworkScript = `#!/bin/bash

NM_CONFIG_DIR=${PATH_PREFIX}/etc/assisted/network
MAC_NIC_MAPPING_FILE=mac_interface.ini
PATH_PREFIX=${PATH_PREFIX:=''}

if [[ ! -d "$NM_CONFIG_DIR" ]]
then
  echo "Error (exiting): Expected to find the directory $NM_CONFIG_DIR on the host but this was not present."
  exit 0
fi

# A map of host mac addresses to interface names
declare -A host_macs_to_hw_iface

# Find destination directory based on ISO mode
if [[ -f ${PATH_PREFIX}/etc/initrd-release ]]; then
  ETC_NETWORK_MANAGER="${PATH_PREFIX}/etc/coreos-firstboot-network"
else
  ETC_NETWORK_MANAGER="${PATH_PREFIX}/etc/NetworkManager/system-connections"
fi
echo "Info: ETC_NETWORK_MANAGER was set to $ETC_NETWORK_MANAGER"

# Create a map of host mac addresses to their network interfaces
function map_host_macs_to_interfaces() {
  SYS_CLASS_NET_DIR="${PATH_PREFIX}/sys/class/net"
  for nic in $( ls $SYS_CLASS_NET_DIR )
  do
    mac=$(cat $SYS_CLASS_NET_DIR/$nic/address | tr '[:lower:]' '[:upper:]')
    [[ -n "$mac" ]] && host_macs_to_hw_iface[$mac]=$nic
  done
}
`
