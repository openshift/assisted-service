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

# Returns a sorted, comma-separated list of non-loopback interface names under sysfs.
function net_interface_names() {
  local fp=""
  for nic in $(ls $SYS_CLASS_NET_DIR 2>/dev/null | sort); do
    [[ "$nic" == "lo" ]] && continue
    fp="${fp}${nic},"
  done
  echo "$fp"
}

# Create a map of host mac addresses to their network interfaces
function map_host_macs_to_interfaces() {
  SYS_CLASS_NET_DIR="${PATH_PREFIX}/sys/class/net"
  local stable_seconds=3
  local max_wait_seconds=30
  local unchanged_seconds=0
  local waited_seconds=0
  local prev_interface_names=""

  while [[ $waited_seconds -lt $max_wait_seconds ]]; do
    current_interface_names="$(net_interface_names)"
    if [[ -n "$prev_interface_names" && "$current_interface_names" == "$prev_interface_names" ]]; then
      unchanged_seconds=$((unchanged_seconds + 1))
      if [[ $unchanged_seconds -ge $stable_seconds ]]; then
        echo "Info: Network interfaces stable for ${stable_seconds} seconds"
        break
      fi
    else
      if [[ -n "$current_interface_names" && "$current_interface_names" != "$prev_interface_names" ]]; then
        echo "Info: Network interfaces discovered: ${current_interface_names}"
      fi
      prev_interface_names="$current_interface_names"
      unchanged_seconds=0
    fi
    sleep 1
    waited_seconds=$((waited_seconds + 1))
  done

  if [[ $unchanged_seconds -lt $stable_seconds ]]; then
    echo "Warning: Timed out after ${max_wait_seconds}s waiting for stable network interfaces, continuing anyway"
  fi

  host_macs_to_hw_iface=()
  for nic in $(ls $SYS_CLASS_NET_DIR 2>/dev/null); do
    [[ "$nic" == "lo" ]] && continue
    mac=$(cat $SYS_CLASS_NET_DIR/$nic/address 2>/dev/null | tr '[:lower:]' '[:upper:]')
    [[ -n "$mac" ]] && host_macs_to_hw_iface[$mac]=$nic
  done
}
`
