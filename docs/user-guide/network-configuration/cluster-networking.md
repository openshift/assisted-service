# Cluster Networking

There are various network types and addresses used by OCP and listed in the table below.

| Type           | DNS                                   | Description                                                                                                                                          |
| -------------- | ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| clusterNetwork |                                       | IP address pools from which pod IP addresses are allocated                                                                                           |
| serviceNetwork |                                       | IP address pool for services                                                                                                                         |
| machineNetwork |                                       | IP address blocks for machines forming the cluster                                                                                                   |
| apiVIP         | api.<clustername.clusterdomain>       | The VIP to use for API communication. This setting must either be provided or pre-configured in the DNS so that the default name resolves correctly. |
| ingressVIP     | test.apps.<clustername.clusterdomain> | The VIP to use for ingress traffic                                                                                                                   |

Apart from this, depending on the desired network stack, different network controllers can be selected. Currently Assisted Service can deploy OCP clusters using one of the following configurations

- IPv4
- IPv6
- Dual-stack (IPv4 + IPv6)

Supported network controllers depend on the selected stack and are summarized in the table below. For a detailed CNI network provider feature comparison you may want to check the [OCP Networking documentation](https://docs.openshift.com/container-platform/4.8/networking/openshift_sdn/about-openshift-sdn.html#nw-ovn-kubernetes-matrix_about-openshift-sdn).

| Stack      | SDN | OVN |
| ---------- | --- | --- |
| IPv4       | Yes | Yes |
| IPv6       | No  | Yes |
| Dual-stack | No  | Yes |

## Limitations

### SDN

- With single node OpenShift (SNO), the SDN controller is not supported.
- The SDN controller does not support IPv6.

### OVN-Kubernetes

https://docs.openshift.com/container-platform/4.10/networking/ovn_kubernetes_network_provider/about-ovn-kubernetes.html#nw-ovn-kubernetes-limitations_about-ovn-kubernetes

## Cluster Network

Cluster network is a network from which every pod deployed in the cluster gets its IP address. Given that the workload may live across multiple nodes forming the cluster, it's important for the network provider to be able to easily find an individual node based on the pod's IP address. In order to do this, `clusterNetwork.cidr` is further split into multiple subnets of the size defined in `clusterNetwork.hostPrefix`.

Host prefix specifies a lenght of the subnet assigned to each individual node in the cluster. An example of how a cluster may assign addresses for the multi-node cluster

```yaml
clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
```

Creating a 3-node cluster using the snippet above may create the following network topology

- pods scheduled in node #1 get IPs from `10.128.0.0/23`
- pods scheduled in node #2 get IPs from `10.128.2.0/23`
- pods scheduled in node #3 get IPs from `10.128.4.0/23`

Explaining OVN-K8s internals is out of scope of this document, but the pattern described above gives us an easy way to route pod-to-pod traffic between different nodes without keeping a big list of mapping between pods and their corresponding nodes.

## Machine Network

Machine network is a network used by all the hosts forming the cluster to communicate with each other. This is also the subnet that has to include the API and Ingress VIPs.

## SNO vs Multi-Node Cluster

Depending on whether a Single Node OpenShift or a Multi-Node cluster is deployed, different values are mandatory. The table below explains this in more detail.

| Parameter      | SNO                       | Multi-Node Cluster with [DHCP mode](dhcp-vip-allocation.md) | Multi-Node Cluster without DHCP mode |
| -------------- | ------------------------- | ----------------------------------------------------------- | ------------------------------------ |
| clusterNetwork | Required                  | Required                                                    | Required                             |
| serviceNetwork | Required                  | Required                                                    | Required                             |
| machineNetwork | Auto-assign possible (\*) | Auto-assign possible (\*)                                   | Auto-assign possible (\*)            |
| apiVIP         | Forbidden                 | Forbidden                                                   | Required                             |
| ingressVIP     | Forbidden                 | Forbidden                                                   | Required                             |

(\*) Auto assignment of machine network CIDR happens if there is only a single host network. Otherwise it has to be selected explicitly.

## Airgapped environments

The workflow for deploying a cluster without Internet access has some prerequisites which are out of scope of this document. You may consult the [Zero Touch Provisioning the hard way git repo](https://github.com/jparrill/ztp-the-hard-way/blob/main/docs/disconnected-ZTP-flow-hub-deployment.md) for some insights.
