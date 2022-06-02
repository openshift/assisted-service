# OVN-Kubernetes Troubleshooting

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
