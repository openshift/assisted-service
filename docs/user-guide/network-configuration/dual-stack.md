# Dual-Stack Network Topology

This configuration allows deployment of a cluster with pods residing in both IPv4 and IPv6 subnets.

## Sample KubeAPI CR

When using Assisted Service on-premises, configuration of the IP stack is done in the [AgentClusterInstall custom resource](../../hive-integration/README.md#AgentClusterInstall).

* [Deploying a SNO cluster](../../hive-integration/crds/agentClusterInstall-dualstack-SNO.yaml)
* [Deploying a multi-node OCP cluster](../../hive-integration/crds/agentClusterInstall-dualstack.yaml)
