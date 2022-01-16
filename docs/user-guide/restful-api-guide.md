# Introduction

The purpose of this guide is to assist users in using the RESTful API for interacting with the assisted-service.

# Setting Static Network Config

User may provide static network configurations when generating the discovery ISO.
The static network configurations per each host should contain:
* [nmstate](https://www.nmstate.io/) file in YAML format, specifying the desired network configuration for the host
  * The file will contain interface logical names that will be replaced with host's actual interface name at discovery time
* A map (mac-interface-mapping) containing the mapping of mac-address to the logical interface name

In the following example `server-a.yaml` and `server-b.yaml` files contain the nmstate configuration in YAML format for two nodes.
Here is an example of the content of `server-a.yaml`, setting network configuration for two of its network interfaces:
```yaml
dns-resolver:
  config:
    server:
    - 192.168.126.1
interfaces:
- ipv4:
    address:
    - ip: 192.168.126.30
      prefix-length: 24
    dhcp: false
    enabled: true
  name: eth0
  state: up
  type: ethernet
- ipv4:
    address:
    - ip: 192.168.141.30
      prefix-length: 24
    dhcp: false
    enabled: true
  name: eth1
  state: up
  type: ethernet
routes:
  config:
  - destination: 0.0.0.0/0
    next-hop-address: 192.168.126.1
    next-hop-interface: eth0
    table-id: 254
```

The `mac-interface-mapping` attribute should map the MAC Addresses of the host to the logical interface name as used in the `network_yaml` element (nmstate files):
```yaml
mac_interface_map: [
    {
      mac_address: 02:00:00:2c:23:a5,
      logical_nic_name: eth0
    },
    {
      mac_address: 02:00:00:68:73:dc,
      logical_nic_name: eth1
    }
]
```

In order to use `curl` to send a request for setting static network configuration, there is a need to JSON-encode the content of those files.
This can be achieved using the `jq` tool as shown below:

```sh
ASSISTED_SERVICE_URL=http://${host_address}:${port}
CLUSTER_ID=...
NODE_SSH_KEY="..."
request_body=$(mktemp)

jq -n --arg SSH_KEY "$NODE_SSH_KEY" --arg NMSTATE_YAML1 "$(cat server-a.yaml)" --arg NMSTATE_YAML2 "$(cat server-b.yaml)" \
'{
  "ssh_public_key": $SSH_KEY,
  "image_type": "full-iso",
  "static_network_config": [
    {
      "network_yaml": $NMSTATE_YAML1,
      "mac_interface_map": [{"mac_address": "02:00:00:2c:23:a5", "logical_nic_name": "eth0"}, {"mac_address": "02:00:00:68:73:dc", "logical_nic_name": "eth1"}]
    },
    {
      "network_yaml": $NMSTATE_YAML2,
      "mac_interface_map": [{"mac_address": "02:00:00:9f:85:eb", "logical_nic_name": "eth1"}, {"mac_address": "02:00:00:c8:be:9b", "logical_nic_name": "eth0"}]
     }
  ]
}' >> $request_body
```
The request will be stored in a temporary file `$request_body` and will be used as the request body of the `curl` command:

```sh
curl -H "Content-Type: application/json" -X POST -d @$request_body ${ASSISTED_SERVICE_URL}/api/assisted-install/v1/clusters/$CLUSTER_ID/downloads/image
```

## Additional nmstate configuration examples
### Tagged VLAN
```yaml
    dns-resolver:
      config:
        server:
        - 192.168.143.1
    interfaces:
    - ipv4:
        address:
        - ip: 192.168.143.15
          prefix-length: 24
        dhcp: false
        enabled: true
      ipv6:
        enabled: false
      name: eth0.404
      state: up
      type: vlan
      vlan:
        base-iface: eth0
        id: 404
    routes:
      config:
      - destination: 0.0.0.0/0
        next-hop-address: 192.168.143.1
        next-hop-interface: eth0.404
        table-id: 254
```
### Network Bond
```yaml
    dns-resolver:
      config:
        server:
        - 192.168.138.1
    interfaces:
    - ipv4:
        address:
        - ip: 192.168.138.15
          prefix-length: 24
        dhcp: false
        enabled: true
      ipv6:
        enabled: false
      link-aggregation:
        mode: active-backup
        options:
          all_slaves_active: delivered
          miimon: "140"
        slaves:
        - eth0
        - eth1
      name: bond0
      state: up
      type: bond
    routes:
      config:
      - destination: 0.0.0.0/0
        next-hop-address: 192.168.138.1
        next-hop-interface: bond0
        table-id: 254
```