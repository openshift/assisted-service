# Single-Stack Network Topology

## Sample KubeAPI CR

When using Assisted Service on-premises, configuration of the IP stack is done in the [AgentClusterInstall custom resource](../../hive-integration/README.md#AgentClusterInstall).

### IPv4

- [Deploying a SNO cluster](../../hive-integration/crds/agentClusterInstall-SNO.yaml)
- [Deploying a multi-node OCP cluster](../../hive-integration/crds/agentClusterInstall.yaml)

### IPv6

Deploying a SNO cluster

- [Deploying a SNO cluster](../../hive-integration/crds/agentClusterInstall-IPv6-SNO.yaml)
- [Deploying a multi-node OCP cluster](../../hive-integration/crds/agentClusterInstall-IPv6.yaml)

## Sample REST API payload

### IPv4

Deploying a SNO cluster

```json
{
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "cluster_networks": [
    {
      "cidr": "10.128.0.0/14",
      "host_prefix": 23
    }
  ],
  "service_networks": [{ "cidr": "172.30.0.0/16" }]
}
```

Deploying a multi-node OCP cluster

```json
{
  "vip_dhcp_allocation": false,
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "api_vip": "192.168.127.100",
  "ingress_vip": "192.168.127.101",
  "cluster_networks": [
    {
      "cidr": "10.128.0.0/14",
      "host_prefix": 23
    }
  ],
  "service_networks": [{ "cidr": "172.30.0.0/16" }]
}
```

```json
{
  "vip_dhcp_allocation": true,
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "cluster_networks": [
    {
      "cidr": "10.128.0.0/14",
      "host_prefix": 23
    }
  ],
  "service_networks": [{ "cidr": "172.30.0.0/16" }]
}
```

### IPv6

Single-stack IPv6 configuration is not allowed in SaaS offering at the moment, therefore we are not providing defaults here.
