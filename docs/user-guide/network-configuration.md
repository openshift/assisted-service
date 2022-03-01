# Network Congfiguration

This document provides an overview of network configurations supported when deploying OCP using Assisted Service.

## NMState

NMStateConfig is a declarative way of managing configuration of networking settings. A general introduction to the NMState can be found in the [upstream documentation](https://nmstate.io/examples.html) whereas the [Hive Integration readme](../hive-integration/README.md#NMStateConfig) explains how to use it together with the Assisted Service.

[This example](../hive-integration/crds/nmstate.yaml) shows how to create a custom NMStateConfig to be used with Assisted Service on-premises.

## OCP Networking

There are various network types and addresses used by OCP and listed in the table below.

| Type           | DNS                                   | Description                                                                                                                                                    |
|----------------|---------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| clusterNetwork |                                       | IP address pools from which pod IP addresses are allocated                                                                                                     |
| serviceNetwork |                                       | IP address pool for services                                                                                                                                   |
| machineNetwork |                                       | IP address blocks for machines forming the cluster                                                                                                             |
| apiVIP         | api.<clustername.clusterdomain>       | The VIP to use for API communication. This setting must either be provided or pre-configured in the DNS so that the default name resolves correctly.  |
| ingressVIP     | test.apps.<clustername.clusterdomain> | The VIP to use for ingress traffic                                                                                                                             |

Apart from this, depending on the desired network stack, different network controller can be selected. Currently Assisted Service can deploy OCP clusters using one of the following configurations

* IPv4
* IPv6 (with all the required images mirrored in some local registry)
* Dual-stack (IPv4 + IPv6)

Supported network controllers depend on the selected stack and are summarized in the table below. For a detailed CNI network provider feature comparison you may want to check the [OCP Networking documentation](https://docs.openshift.com/container-platform/4.8/networking/openshift_sdn/about-openshift-sdn.html#nw-ovn-kubernetes-matrix_about-openshift-sdn).

| Stack      | SDN           | OVN |
|------------|---------------|-----|
| IPv4       | Yes (default) | Yes |
| IPv6       | No            | Yes |
| Dual-stack | No            | Yes |

With single node OpenShift (SNO), the SDN controller is not supported.

### Cluster Network

Cluster network is a network from which every pod deployed in the cluster gets its IP address. Given that the workload may live across multiple nodes forming the cluster, it's important for the network provider to be able to easily find an individual node based on the pod's IP address. In order to do this, `clusterNetwork.cidr` is further split into multiple subnets of the size defined in `clusterNetwork.hostPrefix`.

Host prefix specifies a lenght of the subnet assigned to each individual node in the cluster. An example of how a cluster may assign addresses for the multi-node cluster

```yaml
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
```

Creating a 3-node cluster using the snipet above may create the following network topology

* pods scheduled in the node #1 get IPs from `10.128.0.0/23`
* pods scheduled in the node #2 get IPs from `10.128.2.0/23`
* pods scheduled in the node #3 get IPs from `10.128.4.0/23`

Explaining OVN-K8s internals is out of scope of this document, but the pattern described above gives us an easy way to route pod-to-pod traffic between different nodes without keeping a big list of mapping between pods and their corresponding nodes.

### Additional reading

Additional explanation of the syntax used in the network subnets can be found in the [Installing on bare metal OCP documentation](https://docs.openshift.com/container-platform/4.8/installing/installing_bare_metal/installing-bare-metal-network-customizations.html#installation-configuration-parameters-network_installing-bare-metal-network-customizations). It may also be useful to familiarize yourself with [Cluster Network Operator configuration](https://docs.openshift.com/container-platform/4.8/networking/cluster-network-operator.html#nw-operator-cr-cno-object_cluster-network-operator).

[Bare metal IPI documentation](https://docs.openshift.com/container-platform/4.8/installing/installing_bare_metal_ipi/ipi-install-installation-workflow.html#additional-install-config-parameters_ipi-install-configuration-files) provides additional explanation of the syntax for the VIP addresses.

### SNO vs Multi-Node Cluster

Depending whether a Single Node OpenShift or a Multi-Node cluster is deployed, different values are mandatory. The table below explains this in more detail.

| Parameter      | SNO       | Multi-Node Cluster     |
|----------------|-----------|------------------------|
| clusterNetwork | Required  | Required               |
| serviceNetwork | Required  | Required               |
| machineNetwork | Required  | Forbidden in DHCP mode |
| apiVIP         | Forbidden | Required               |
| ingressVIP     | Forbidden | Required               |

## IP Stack

When using Assisted Service on-premises, configuration of the IP stack is done in the [AgentClusterInstall custom resource](../hive-integration/README.md#AgentClusterInstall). Examples below show how different configurations can be achieved.

### IPv4

Sample CR for deploying a SNO cluster using Assisted Service. In this scenario only one subnet per network type is supported.

```yaml
  networking:
    networkType: OpenShiftSDN
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    machineNetwork:
    - cidr: 192.168.126.0/24
    serviceNetwork:
    - 172.30.0.0/16
```

Sample CR for multi-node OCP cluster

```yaml
spec:
  apiVIP: 1.2.3.8
  ingressVIP: 1.2.3.9
  networking:
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    serviceNetwork:
    - 172.30.0.0/16
```

### IPv6

The workflow for deploying a cluster without Internet access has some prerequisites which are out of scope of this document. You may consult the [Zero Touch Provisioning in the hard way git repo](https://github.com/jparrill/ztp-the-hard-way/blob/main/docs/disconnected-ZTP-flow-hub-deployment.md) for some insights.

Sample CR for deploying a SNO cluster using Assisted Service. In this scenario only one subnet per network type is supported.

```yaml
spec:
  networking:
    networkType: OVNKubernetes
    clusterNetwork:
    - cidr: fd01::/48
      hostPrefix: 64
    machineNetwork:
    - cidr: 1001:db8::/120
    serviceNetwork:
    - fd02::/112
```

Sample CR for multi-node OCP cluster

```yaml
spec:
  apiVIP: "2620:52:0:1302::3"
  ingressVIP: "2620:52:0:1302::2"
  networking:
    clusterNetwork:
    - cidr: fd01::/48
      hostPrefix: 64
    serviceNetwork:
    - fd02::/112
```

### Dual-stack

This configuration allows deployment of a cluster with pods residing in both IPv4 and IPv6 subnets. Be aware of the following limitations and requirements of this configuration.

* `apiVIP` and `ingressVIP` support only single value. Both addresses must come from the IPv4 range (the work is being done by the Metal Platform Networking to support multiple addresses)
* `clusterNetwork`, `machineNetwork` and `serviceNetwork` support exactly 2 values. The first one must come from the IPv4 range, the second one from the IPv6 (i.e. dual-stack setups must be "IPv4-primary")

Sample CR for deploying a SNO cluster using Assisted Service

```yaml
spec:
  networking:
    networkType: OVNKubernetes
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    - cidr: fd01::/48
      hostPrefix: 64
    machineNetwork:
    - cidr: 192.168.126.0/24
    - cidr: 1001:db8::/120
    serviceNetwork:
    - 172.30.0.0/16
    - fd02::/112
```

Sample CR for multi-node OCP cluster

```yaml
spec:
  apiVIP: 1.2.3.8
  ingressVIP: 1.2.3.9
  networking:
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    - cidr: fd01::/48
      hostPrefix: 64
    serviceNetwork:
    - 172.30.0.0/16
    - fd02::/112
```

## OVN-Kubernetes Troubleshooting

For the cluster installed using `networkType: OVNKubernetes` the following steps may be useful to see the network configuration after the deployment.

```sh
# oc -n openshift-ovn-kubernetes get pod/ovnkube-master-9ljfk -o yaml | less

metadata:
  annotations:
    networkoperator.openshift.io/ip-family-mode: dual-stack
...
      exec /usr/bin/ovnkube \
        --metrics-bind-address "127.0.0.1:29102" \
...
        --sb-address "ssl:192.168.126.10:9642,ssl:192.168.126.12:9642" \
...
        --nb-address "ssl:192.168.126.10:9641,ssl:192.168.126.12:9641" \
...
  hostIP: 192.168.126.12
  phase: Running
  podIP: 192.168.126.12
  podIPs:
  - ip: 192.168.126.12
  - ip: 1001:db8::46
```

```sh
# oc describe pod/ovnkube-master-9ljfk -n openshift-ovn-kubernetes | less

Annotations:          networkoperator.openshift.io/ip-family-mode: dual-stack
                      workload.openshift.io/warning: only single-node clusters support workload partitioning
Status:               Running
IP:                   192.168.126.12
IPs:
  IP:           192.168.126.12
  IP:           1001:db8::46
Controlled By:  DaemonSet/ovnkube-master
```

```sh
# oc describe network.config.openshift.io | less

Spec:
  Cluster Network:
    Cidr:         fd01::/48
    Host Prefix:  64
    Cidr:         10.128.0.0/14
    Host Prefix:  23
  External IP:
    Policy:
  Network Type:  OVNKubernetes
  Service Network:
    fd02::/112
    172.30.0.0/16
Status:
  Cluster Network:
    Cidr:               fd01::/48
    Host Prefix:        64
    Cidr:               10.128.0.0/14
    Host Prefix:        23
  Cluster Network MTU:  1400
  Network Type:         OVNKubernetes
  Service Network:
    fd02::/112
    172.30.0.0/16
```

```sh
# oc get nodes test-infra-cluster-assisted-installer-master-0 -o yaml | less

metadata:
  annotations:
    k8s.ovn.org/host-addresses: '["1001:db8:0:200::78","1001:db8::5f","192.168.126.10","192.168.126.101","192.168.141.10"]'
    k8s.ovn.org/l3-gateway-config: '{"default":{"mode":"shared","interface-id":"br-ex_test-infra-cluster-assisted-installer-master-0","mac-address":"02:00:00:af:e4:63","ip-addresses":["192.168.126.10/24","1001:db8::5f/128"],"next-hops":["192.168.126.1","fe80::5054:ff:fe46:98d6"],"node-port-enable":"true","vlan-id":"0"}}'
    k8s.ovn.org/node-chassis-id: 1f22cd5d-b353-4be0-aa25-0f39a3e34519
    k8s.ovn.org/node-mgmt-port-mac-address: 6a:46:7e:d8:aa:d5
    k8s.ovn.org/node-primary-ifaddr: '{"ipv4":"192.168.126.10/24","ipv6":"1001:db8::5f/128"}'
    k8s.ovn.org/node-subnets: '{"default":["10.128.0.0/23","fd01:0:0:1::/64"]}'
    k8s.ovn.org/topology-version: "4"
...
status:
  addresses:
  - address: 192.168.126.10
    type: InternalIP
  - address: 1001:db8::5f
    type: InternalIP
  - address: test-infra-cluster-assisted-installer-master-0
    type: Hostname
...
```
