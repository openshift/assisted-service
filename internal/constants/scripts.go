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
gateway=$FOUND_GW4

[ipv6]
method=$METHOD6
addr-gen-mode=eui64
addresses=$FOUND_IP6/$FOUND_MASK6
dns=$FOUND_DNS6
gateway=$FOUND_GW6

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
                    export FOUND_GW4=$(echo ${entry} | cut -f5 -d\;)
                else
                    export METHOD4="auto"
                fi

                IPV6=$(echo ${entry} | cut -f6 -d\;)
                if [[ ! -z ${IPV6} ]]; then
                    export METHOD6="manual"
                    export FOUND_IP6=${IPV6}
                    export FOUND_MASK6=$(echo ${entry} | cut -f7 -d\;)
                    export FOUND_DNS6=$(echo ${entry} | cut -f8 -d\;)
                    export FOUND_GW6=$(echo ${entry} | cut -f9 -d\;)
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
    for INTERFACE in $(ls -1 /sys/class/net | grep -v lo)
    do
        INT_MAC=$(cat /sys/class/net/${INTERFACE}/address)
        if [[ ! -z ${INT_MAC} ]]; then
            echo "MAC to check: ${INT_MAC}"
            find_my_mac ${INT_MAC}
            if [[ "${FOUND_MAC,,}" == "${INT_MAC,,}" ]]; then
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
