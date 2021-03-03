package constants

// PreNetworkConfigScript script runs on hosts before network manager service starts in order to copy
// the host specifics *.nmconnection files to /NetworkManager/system-connections/ based on host MAC-address matching
const PreNetworkConfigScript = `
#!/bin/bash

# directory containing nmconnection files of all nodes
NMCONNECTIONS_DIR=/etc/assisted/network
if [ ! -d "$NMCONNECTIONS_DIR" ]
then
  exit 0
fi

if [[ $(ls -l ${NMCONNECTIONS_DIR} | grep -c nmconnection) -eq 0 ]]
then
  exit 0
fi

# array storing all host mac addresses
host_macs=()

# find destination directory based on ISO mode
if [[ -f /etc/initrd-release ]]; then
  ETC_NETWORK_MANAGER="/run/NetworkManager/system-connections"
else
  ETC_NETWORK_MANAGER="/etc/NetworkManager/system-connections"
fi

# list host mac addresses for active network interfaces
function list_macs() {
  SYS_CLASS_NET_DIR='/sys/class/net'
  for nic in $( ls $SYS_CLASS_NET_DIR )
  do
    mac=$(cat $SYS_CLASS_NET_DIR/$nic/address | tr '[:lower:]' '[:upper:]')
    host_macs+=($mac)
  done
}

function copy_connection_files_by_mac_address() {
  # iterate over nmconnection files to find associated file with current host
  for nmconn_file in $(ls -1 ${NMCONNECTIONS_DIR}/*.nmconnection)
  do
    # get mac-address from connection files to search by
    mac_address=$(grep -A1 '\[802-3-ethernet\].*' $nmconn_file | grep -v "802-3-ethernet" | cut -d= -f2 | tr '[:lower:]' '[:upper:]')
    echo "mac adddress of ${nmconn_file} is ${mac_address}"

    # compare mac-address from nmconnection with host's mac addresses
    if [[ " ${host_macs[@]} " =~ " ${mac_address} " ]]; then
      echo "copying ${nmconn_file} to NM working directory ${ETC_NETWORK_MANAGER}"
      cp $nmconn_file ${ETC_NETWORK_MANAGER}/
    fi
  done
}

list_macs
copy_connection_files_by_mac_address
`
