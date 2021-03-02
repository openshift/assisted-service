# Introduction

The purpose of this guide is to assist users in using the RESTful API for interacting with the assisted-service.

# Setting Static Network Config

User may provide static network configurations when generating the discovery ISO.
The input for the static network configurations is a set of [nmstate](https://www.nmstate.io/) files in YAML format, each file contains the desired network configuration of a single node.

In the following example `server-a.yaml` and `server-b.yaml` files contain the nmstate configuration in YAML format for two nodes.
Here is an example of the content of `server-a.yaml`, setting network configuration for two of its network interfaces:
```
interfaces:
- name: 020000e7f670
  type: ethernet
  state: up
  ethernet:
    auto-negotiation: true
    duplex: full
    speed: 1000
  ipv4:
    address:
    - ip: 192.168.126.10
      prefix-length: 24
    auto-dns: true
    auto-gateway: true
    auto-routes: true
    dhcp: true
    enabled: true
  mac-address: 02:00:00:e7:f6:70
  mtu: 1500
- name: 020000242e01
  type: ethernet
  state: up
  ethernet:
    auto-negotiation: true
    duplex: full
    speed: 1000
  ipv4:
    enabled: false
  mac-address: 02:00:00:24:2e:01
  mtu: 1500
```
In order to use `curl` to send a request for setting static network configuration, there is a need to JSON-encode the content of those files.
This can be achieved using the `jq` tool as shown below:

```
ASSISTED_SERVICE_URL=http://${host_address}:${port}
CLUSTER_ID=...
NODE_SSH_KEY="..."
request_body=$(mktemp)

jq -n --arg SSH_KEY "$NODE_SSH_KEY" --arg NMSTATE_YAML1 "$(cat server-a.yaml)" --arg NMSTATE_YAML2 "$(cat server-b.yaml)" \
'{
  "ssh_public_key": $SSH_KEY,
  "image_type": "full-iso",
  "static_network_config": [ $NMSTATE_YAML1, $NMSTATE_YAML2 ]
}' >> $request_body
```
The request will be stored in a temporary file `$request_body` and will be used as the request body of the `curl` command:

```
curl -H "Content-Type: application/json" -X POST -d @$request_body ${ASSISTED_SERVICE_URL}/api/assisted-install/v1/clusters/$CLUSTER_ID/downloads/image
```