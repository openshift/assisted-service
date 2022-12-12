# Dual-Stack Network Topology

This configuration allows deployment of a cluster with pods residing in both IPv4 and IPv6 subnets.

## Sample KubeAPI CR

When using Assisted Service on-premises, configuration of the IP stack is done in the [AgentClusterInstall custom resource](../../hive-integration/README.md#AgentClusterInstall).

- [Deploying a SNO cluster](../../hive-integration/crds/agentClusterInstall-dualstack-SNO.yaml)
- [Deploying a multi-node OCP cluster](../../hive-integration/crds/agentClusterInstall-dualstack.yaml)

## Sample REST API payload

Deploying a SNO cluster

```json
{
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "cluster_networks": [
    {
      "cidr": "10.128.0.0/14",
      "host_prefix": 23
    },
    {
      "cidr": "fd01::/48",
      "host_prefix": 64
    }
  ],
  "service_networks": [{ "cidr": "172.30.0.0/16" }, { "cidr": "fd02::/112" }],
  "machine_networks": [
    { "cidr": "192.168.127.0/24" },
    { "cidr": "1001:db8::/120" }
  ]
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
    },
    {
      "cidr": "fd01::/48",
      "host_prefix": 64
    }
  ],
  "service_networks": [{ "cidr": "172.30.0.0/16" }, { "cidr": "fd02::/112" }],
  "machine_networks": [
    { "cidr": "192.168.127.0/24" },
    { "cidr": "1001:db8::/120" }
  ]
}
```

## Limitations

The API VIP IP address and the Ingress VIP address must be of the primary IP address family when using dual-stack networking. Currently, Red Hat does not support dual-stack VIPs or dual-stack networking with IPv6 as the primary IP address family. However, Red Hat does support dual-stack networking with IPv4 as the primary IP address family. Therefore, the IPv4 entries must go before the IPv6 entries.

Source: https://docs.openshift.com/container-platform/4.9/installing/installing_bare_metal_ipi/ipi-install-installation-workflow.html#modifying-install-config-for-dual-stack-network_ipi-install-installation-workflow

## Workload

You can find samples of Kubernetes resources using dual-stack services you can refer to the offical kubernetes documentation available at https://kubernetes.io/docs/concepts/services-networking/dual-stack/#dual-stack-service-configuration-scenarios
