# Dual-Stack Network Topology

This configuration allows deployment of a cluster with pods residing in both IPv4 and IPv6 subnets.

## Sample KubeAPI CR

When using Assisted Service on-premises, configuration of the IP stack is done in the [AgentClusterInstall custom resource](../../hive-integration/README.md#AgentClusterInstall).

* [Deploying a SNO cluster](../../hive-integration/crds/agentClusterInstall-dualstack-SNO.yaml)
* [Deploying a multi-node OCP cluster](../../hive-integration/crds/agentClusterInstall-dualstack.yaml)

## Primary Stack

Primary IP stack is calculated when the cluster is first created. 

When specifying the IP addresses or CIDRs for each network field, the primary stack should be the first network family in each respective field, followed by the secondary stack.

For example, for an IPv4 primary stack cluster, the cluster fields might appear like this:

```
MachineNetworks: []*models.MachineNetwork{
    {Cidr: "10.0.0.0/16"}, // IPv4 first
    {Cidr: "2001:db8::/64"},
},
APIVips: []*models.APIVip{
    {IP: "10.0.1.1"}, // IPv4 first
    {IP: "2001:db8::1"},
},
ServiceNetworks: []*models.ServiceNetwork{
    {Cidr: "172.30.0.0/16"}, // IPv4 first
    {Cidr: "2001:db8:1::/64"},
},
ClusterNetworks: []*models.ClusterNetwork{
    {Cidr: "10.128.0.0/14"}, // IPv4 first
    {Cidr: "2001:db8:2::/64"},
},
```

Notice that for every network field, the IPv4 address is first. Assisted uses this order to determine the primary IP stack for a cluster.

The order should be maintained if a network field is updated.

## Support versions

IPv6 primary IP support starts at OCP 4.12.0 for multi-node clusters and at OCP 4.19.0 for SNO clusters. 
Prior to these versions, only IPv4 primary for dual-stack is supported.
