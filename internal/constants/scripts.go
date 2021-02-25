package constants

const ConfigStaticIpsScript = `
#!/bin/bash

function write_connection_file() {
    if [[ -f /etc/initrd-release ]]; then
        NM_KEY_FILE="/run/NetworkManager/system-connections/${FOUND_INTERFACE}.nmconnection"
    else
        NM_KEY_FILE="/etc/NetworkManager/system-connections/${FOUND_INTERFACE}.nmconnection"
    fi
    cat > ${NM_KEY_FILE} <<EOF
[connection]
id=$FOUND_INTERFACE
interface-name=$FOUND_INTERFACE
type=ethernet
multi-connect=3
autoconnect=true
autoconnect-priority=1

[ethernet]
mac-address-blacklist=

[ipv4]
method=$METHOD4
addr-gen-mode=eui64
addresses=$FOUND_IP4/$FOUND_MASK4
dns=$FOUND_DNS4
$FOUND_GW4_GATEWAY
$FOUND_ROUTE_METRIC

[ipv6]
method=$METHOD6
addr-gen-mode=eui64
addresses=$FOUND_IP6/$FOUND_MASK6
dns=$FOUND_DNS6
$FOUND_GW6_GATEWAY
$FOUND_ROUTE_METRIC

[802-3-ethernet]
mac-address=$FOUND_MAC
EOF
    chmod 600 ${NM_KEY_FILE}
}

function find_my_mac() {
    MAC_TO_CHECK=${1}

    unset FOUND_MAC
    for entry in $(cat "/etc/static_ips_config.csv")
    do
        MAC=$(echo ${entry} | cut -f1 -d\;)
        if [[ ! -z ${MAC} ]] && [[ -z ${FOUND_MAC} ]]; then
            if [[ "${MAC,,}" == "${MAC_TO_CHECK,,}" ]]; then
                export FOUND_INTERFACE=${INTERFACE}
                export FOUND_MAC=${MAC}

                IPV4=$(echo ${entry} | cut -f2 -d\;)
                if [[ ! -z ${IPV4} ]]; then
                    export METHOD4="manual"
                    export FOUND_IP4=${IPV4}
                    export FOUND_MASK4=$(echo ${entry} | cut -f3 -d\;)
                    export FOUND_DNS4=$(echo ${entry} | cut -f4 -d\;)
		    GW4=$(echo ${entry} | cut -f5 -d\;)
		    if [[ ! -z ${GW4} ]]; then
		        FOUND_GW4_GATEWAY="gateway=${GW4}"
		    else
		        unset FOUND_GW4_GATEWAY
                    fi
                else
                    export METHOD4="auto"
                fi

                IPV6=$(echo ${entry} | cut -f6 -d\;)
                if [[ ! -z ${IPV6} ]]; then
                    export METHOD6="manual"
                    export FOUND_IP6=${IPV6}
                    export FOUND_MASK6=$(echo ${entry} | cut -f7 -d\;)
                    export FOUND_DNS6=$(echo ${entry} | cut -f8 -d\;)
		    GW6=$(echo ${entry} | cut -f9 -d\;)
		    if [[ ! -z ${GW6} ]]; then
		        FOUND_GW6_GATEWAY="gateway=${GW6}"
	            else
		        unset FOUND_GW6_GATEWAY
                    fi
                else
                    export METHOD6="auto"
                fi

                break
            fi
        fi
    done

    if [[ -z ${FOUND_MAC} ]]; then
        echo "Host MAC ${MAC_TO_CHECK} not found in the list"
    fi
}

function correlate_int_mac() {
    # Correlate the Mac with the interface
    METRIC_KEY="route-metric="
    METRIC_VAL=100
    for INTERFACE in $(ls -1 /sys/class/net | grep -v lo)
    do
        INT_MAC=$(cat /sys/class/net/${INTERFACE}/address)
        if [[ ! -z ${INT_MAC} ]]; then
            echo "MAC to check: ${INT_MAC}"
            find_my_mac ${INT_MAC}
            if [[ "${FOUND_MAC,,}" == "${INT_MAC,,}" ]]; then
                export FOUND_ROUTE_METRIC=$METRIC_KEY$METRIC_VAL
                let METRIC_VAL+=1
                echo "MAC Found in the list, this is the Net data: "
                echo "MAC: ${FOUND_MAC}"
                echo "IPv4 Address: ${FOUND_IP4}"
                echo "IPv4 MASK: ${FOUND_MASK4}"
                echo "IPv4 GW: ${FOUND_GW4}"
                echo "IPv4 DNS: ${FOUND_DNS4}"
                echo "IPv6 IP: ${FOUND_IP6}"
                echo "IPv6 MASK: ${FOUND_MASK6}"
                echo "IPv6 GW: ${FOUND_GW6}"
                echo "IPv6 DNS: ${FOUND_DNS6}"
                echo "FOUND_ROUTE_METRIC: ${FOUND_ROUTE_METRIC}"

                echo "Configuring interface ${FOUND_INTERFACE}, MAC address ${FOUND_MAC} with IPv4: ${FOUND_IP4}, IPv6: ${FOUND_IP6}"
                write_connection_file
            fi
        else
            echo "Could not extract MAC address from interface ${INTERFACE}"
        fi
    done
}

correlate_int_mac
`

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

if [[ $(find ${NMCONNECTIONS_DIR} -type f -name "*.nmconnection" | wc -l) -eq 0 ]]
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

    # compare mac-address from nmconnection with host's mac addresses
    if [[ " ${host_macs[@]} " =~ " ${mac_address} " ]]; then
      cp $nmconn_file ${ETC_NETWORK_MANAGER}/
    fi
  done
}

list_macs
copy_connection_files_by_mac_address
`
