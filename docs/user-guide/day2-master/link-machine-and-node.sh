#!/bin/bash

# Credit goes to https://bugzilla.redhat.com/show_bug.cgi?id=1801238.
# This script will link Machine object and Node object. This is needed
# in order to have IP address of the Node present in the status of the Machine.

set -x
set -e

machine="$1"
node="$2"

if [ -z "$machine" -o -z "$node" ]; then
    echo "Usage: $0 MACHINE NODE"
    exit 1
fi

uid=$(echo $node | cut -f1 -d':')
node_name=$(echo $node | cut -f2 -d':')

oc proxy &
proxy_pid=$!
function kill_proxy() {
    kill $proxy_pid
}
trap kill_proxy EXIT SIGINT

HOST_PROXY_API_PATH="http://localhost:8001/apis/metal3.io/v1alpha1/namespaces/openshift-machine-api/baremetalhosts"

function wait_for_json() {
    local name
    local url
    local curl_opts
    local timeout

    local start_time
    local curr_time
    local time_diff

    name="$1"
    url="$2"
    timeout="$3"
    shift 3
    curl_opts="$@"
    echo -n "Waiting for $name to respond"
    start_time=$(date +%s)
    until curl -g -X GET "$url" "${curl_opts[@]}" 2>/dev/null | jq '.' 2>/dev/null >/dev/null; do
        echo -n "."
        curr_time=$(date +%s)
        time_diff=$((curr_time - start_time))
        if [[ $time_diff -gt $timeout ]]; then
            echo "\nTimed out waiting for $name"
            return 1
        fi
        sleep 5
    done
    echo " Success!"
    return 0
}
wait_for_json oc_proxy "${HOST_PROXY_API_PATH}" 10 -H "Accept: application/json" -H "Content-Type: application/json"

addresses=$(oc get node -n openshift-machine-api ${node_name} -o json | jq -c '.status.addresses')

machine_data=$(oc get machine -n openshift-machine-api -o json ${machine})
host=$(echo "$machine_data" | jq '.metadata.annotations["metal3.io/BareMetalHost"]' | cut -f2 -d/ | sed 's/"//g')

if [ -z "$host" ]; then
    echo "Machine $machine is not linked to a host yet." 1>&2
    exit 1
fi

# The address structure on the host doesn't match the node, so extract
# the values we want into separate variables so we can build the patch
# we need.
hostname=$(echo "${addresses}" | jq '.[] | select(. | .type == "Hostname") | .address' | sed 's/"//g')
ipaddr=$(echo "${addresses}" | jq '.[] | select(. | .type == "InternalIP") | .address' | sed 's/"//g')

host_patch='
{
  "status": {
    "hardware": {
      "hostname": "'${hostname}'",
      "nics": [
        {
          "ip": "'${ipaddr}'",
          "mac": "00:00:00:00:00:00",
          "model": "unknown",
          "speedGbps": 10,
          "vlanId": 0,
          "pxe": true,
          "name": "eth1"
        }
      ],
      "systemVendor": {
        "manufacturer": "Red Hat",
        "productName": "product name",
        "serialNumber": ""
      },
      "firmware": {
        "bios": {
          "date": "04/01/2014",
          "vendor": "SeaBIOS",
          "version": "1.11.0-2.el7"
        }
      },
      "ramMebibytes": 0,
      "storage": [],
      "cpu": {
        "arch": "x86_64",
        "model": "Intel(R) Xeon(R) CPU E5-2630 v4 @ 2.20GHz",
        "clockMegahertz": 2199.998,
        "count": 4,
        "flags": []
      }
    }
  }
}
'

echo "PATCHING HOST"
echo "${host_patch}" | jq .

curl -s \
    -X PATCH \
    ${HOST_PROXY_API_PATH}/${host}/status \
    -H "Content-type: application/merge-patch+json" \
    -d "${host_patch}"

oc get baremetalhost -n openshift-machine-api -o yaml "${host}"
