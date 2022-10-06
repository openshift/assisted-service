# Adding 4th master node on OpenShift 4.11 (and newer) with 3 healthy nodes

## Add day-2 master

Please follow the pre-existing guide describing the process of adding a day 2 node. Please note that you need to annotate the agent with `role: master`.

A sample guide for adding a day-2 node is available [here](../rest-api-day2.md).

## Accept CSRs

During the installation there may be some CSRs to approve:

```
[root@test1001 ~]# oc get csr
NAME        AGE     SIGNERNAME                                    REQUESTOR                                                                   REQUESTEDDURATION   CONDITION
csr-sc2rk   73s     kubernetes.io/kubelet-serving                 system:node:extraworker-5                                                   <none>              Approved,Issued
csr-tv9rs   7m59s   kubernetes.io/kube-apiserver-client-kubelet   system:serviceaccount:openshift-machine-config-operator:node-bootstrapper   <none>              Approved,Issued
```

## Confirm 4th master node is ready

```
[root@test1001 ~]# oc get nodes
NAME            STATUS   ROLES    AGE     VERSION
extraworker-0   Ready    master   4h42m   v1.24.0+3882f8f
extraworker-1   Ready    worker   4h29m   v1.24.0+3882f8f
extraworker-2   Ready    master   4h43m   v1.24.0+3882f8f
extraworker-3   Ready    master   4h27m   v1.24.0+3882f8f
extraworker-4   Ready    worker   4h30m   v1.24.0+3882f8f
extraworker-5   Ready    master   105s    v1.24.0+3882f8f
```

## Ensure correct Machine, Node and BareMetalHost CRs (only if running with Machine API)

If the cluster is running with [functional Machine API](https://github.com/openshift/enhancements/blob/master/enhancements/etcd/protecting-etcd-quorum-during-control-plane-scaling.md#when-is-machine-api-functional), etcd-operator requires Machine objects to be present. As those may not always be created as part of the day-2 process, we need to make sure those exist. The simplest way to distinguish between functional and non-functional Machine API is to look for presence or absence of Machines in the `Running` phase.

### Create Machine object linked with BMH and Node

Make sure there is a Machine object referencing the newly added node. If you are deploying using e.g. Zero Touch Provisioning, this step may not be needed. Boot-it-yourself will not create BareMetalHost and Machine so you need to make sure they exist. Otherwise etcd-operator will not do its job correctly.

A signature of etcd-operator missing Machine object is shown below:

```
$ oc logs -n openshift-etcd-operator etcd-operator-8668df65d-rnwmc
[...]
I0926 16:51:23.727603       1 clustermembercontroller.go:203] Ignoring node (extraworker-5) for scale-up: no Machine found referencing this node's internal IP (192.168.111.28)
```

You can also see that etcd pods on the newly added master are crashlooping:

```
[root@test1001 ~]# oc get pods -n openshift-etcd | grep -v etcd-guard | grep etcd
etcd-extraworker-0                 5/5     Running            0             4h28m
etcd-extraworker-2                 5/5     Running            0             4h26m
etcd-extraworker-3                 5/5     Running            0             4h24m
etcd-extraworker-5                 4/5     CrashLoopBackOff   4 (55s ago)   11m
```

Use the instruction below to create BMH and Machine that simulate the objects that would otherwise be created by ZTP. Please note that not all the values need to reflect real configuration of the system.

### Add BareMetalHost object

In the CR below you don't care about values, they can be fake. Remember the `.metadata.name` as it will be used later.

```
---
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: custom-master3-chocobomb
  namespace: openshift-machine-api
  annotations:
spec:
  automatedCleaningMode: metadata
  bootMACAddress: 00:00:00:00:00:02
  bootMode: UEFI
  customDeploy:
    method: install_coreos
  externallyProvisioned: true
  online: true
  userData:
    name: master-user-data-managed
    namespace: openshift-machine-api
```

### Add Machine object

In the CR below you need to use the BMH's name from above in the `.metadata.annotations`. The cluster name under `.metadata.labels` needs to be correct. You can get it by looking at the output of e.g. `oc get machine -A`. The `.metadata.name` does not have to be the same as the name of BMH above, but for simplicity you can use the same name.

```
---
apiVersion: machine.openshift.io/v1beta1
kind: Machine
metadata:
  annotations:
    machine.openshift.io/instance-state: externally provisioned
    metal3.io/BareMetalHost: openshift-machine-api/custom-master3-chocobomb
  finalizers:
  - machine.machine.openshift.io
  generation: 3
  labels:
    machine.openshift.io/cluster-api-cluster: test-day2-1-6qv96
    machine.openshift.io/cluster-api-machine-role: master
    machine.openshift.io/cluster-api-machine-type: master
  name: custom-master3-chocobomb
  namespace: openshift-machine-api
spec:
  metadata: {}
  providerSpec:
    value:
      apiVersion: baremetal.cluster.k8s.io/v1alpha1
      customDeploy:
        method: install_coreos
      hostSelector: {}
      image:
        checksum: ""
        url: ""
      kind: BareMetalMachineProviderSpec
      metadata:
        creationTimestamp: null
      userData:
        name: master-user-data-managed
```

### Link BMH and Machine and Node using the magic script

We need the Machine object to have the IP of the Node in its `.status` field. This is because of how etcd-operator accepts members of the cluster. In order to achieve that, a link between BareMetalHost, Machine and Node is needed. The script below takes care of it:

```
# bash link-machine-and-node.sh custom-master3-2 extraworker-5
```

## Confirm members of etcd

Execute in the scope of one of the old masters:

```
[root@test1001 ~]# oc rsh -n openshift-etcd etcd-extraworker-2
sh-4.4# etcdctl member list -w table
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| 61e2a86084aafa62 | started | extraworker-2 | https://192.168.111.25:2380 | https://192.168.111.25:2379 |      false |
| aee882f5dfbef5e4 | started | extraworker-0 | https://192.168.111.23:2380 | https://192.168.111.23:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
```

## Wait for etcd-operator

It takes a while for the operator to rollout configuration to all the nodes. You can monitor the process as below:

```
[root@test1001 ~]# oc get clusteroperator etcd
NAME   VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
etcd   4.11.5    True        True          False      5h49m   NodeInstallerProgressing: 2 nodes are at revision 10; 1 nodes are at revision 11; 1 nodes are at revision 13
```

After finished it should look like below:

```
[root@test1001 ~]# oc get clusteroperator etcd
NAME   VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
etcd   4.11.5    True        False         False      5h54m
```

## Confirm health of the etcd

```
[root@test1001 ~]# oc rsh -n openshift-etcd etcd-extraworker-0
sh-4.4# etcdctl endpoint health
https://192.168.111.23:2379 is healthy: successfully committed proposal: took = 11.133042ms
https://192.168.111.26:2379 is healthy: successfully committed proposal: took = 11.297561ms
https://192.168.111.28:2379 is healthy: successfully committed proposal: took = 11.870755ms
https://192.168.111.25:2379 is healthy: successfully committed proposal: took = 13.892416ms
```

## Confirm health of the cluster

```
[root@test1001 ~]# oc get Nodes
NAME            STATUS   ROLES    AGE     VERSION
extraworker-0   Ready    master   6h20m   v1.24.0+3882f8f
extraworker-1   Ready    worker   6h7m    v1.24.0+3882f8f
extraworker-2   Ready    master   6h20m   v1.24.0+3882f8f
extraworker-3   Ready    master   6h4m    v1.24.0+3882f8f
extraworker-4   Ready    worker   6h7m    v1.24.0+3882f8f
extraworker-5   Ready    master   99m     v1.24.0+3882f8f
```

```
[root@test1001 ~]# oc get ClusterOperators
NAME                                       VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
authentication                             4.11.5    True        False         False      5h57m
baremetal                                  4.11.5    True        False         False      6h19m
cloud-controller-manager                   4.11.5    True        False         False      6h20m
cloud-credential                           4.11.5    True        False         False      6h23m
cluster-autoscaler                         4.11.5    True        False         False      6h18m
config-operator                            4.11.5    True        False         False      6h19m
console                                    4.11.5    True        False         False      6h4m
csi-snapshot-controller                    4.11.5    True        False         False      6h19m
dns                                        4.11.5    True        False         False      6h18m
etcd                                       4.11.5    True        False         False      6h17m
image-registry                             4.11.5    True        False         False      6h7m
ingress                                    4.11.5    True        False         False      6h6m
insights                                   4.11.5    True        False         False      6h12m
kube-apiserver                             4.11.5    True        False         False      6h16m
kube-controller-manager                    4.11.5    True        False         False      6h16m
kube-scheduler                             4.11.5    True        False         False      6h16m
kube-storage-version-migrator              4.11.5    True        False         False      6h19m
machine-api                                4.11.5    True        False         False      6h15m
machine-approver                           4.11.5    True        False         False      6h19m
machine-config                             4.11.5    True        False         False      6h18m
marketplace                                4.11.5    True        False         False      6h18m
monitoring                                 4.11.5    True        False         False      6h4m
network                                    4.11.5    True        False         False      6h20m
node-tuning                                4.11.5    True        False         False      6h18m
openshift-apiserver                        4.11.5    True        False         False      6h8m
openshift-controller-manager               4.11.5    True        False         False      6h7m
openshift-samples                          4.11.5    True        False         False      6h12m
operator-lifecycle-manager                 4.11.5    True        False         False      6h18m
operator-lifecycle-manager-catalog         4.11.5    True        False         False      6h19m
operator-lifecycle-manager-packageserver   4.11.5    True        False         False      6h12m
service-ca                                 4.11.5    True        False         False      6h19m
storage                                    4.11.5    True        False         False      6h19m
```

```
[root@test1001 ~]# oc get ClusterVersion
NAME      VERSION   AVAILABLE   PROGRESSING   SINCE   STATUS
version   4.11.5    True        False         5h57m   Cluster version is 4.11.5
```

## Remove one of the old masters

Delete the BareMetalHost object:

```
[root@test1001 ~]# oc delete bmh -n openshift-machine-api   extraworker-0
```

Confirm the Machine is unhealthy:

```
[root@test1001 ~]# oc get machine -A
NAMESPACE               NAME                               PHASE     TYPE   REGION   ZONE   AGE
openshift-machine-api   custom-master3-2                   Running                          14h
openshift-machine-api   test-day2-1-6qv96-master-0         Failed                           20h
openshift-machine-api   test-day2-1-6qv96-master-1         Running                          20h
openshift-machine-api   test-day2-1-6qv96-master-2         Running                          20h
openshift-machine-api   test-day2-1-6qv96-worker-0-8w7vr   Running                          19h
openshift-machine-api   test-day2-1-6qv96-worker-0-rxddj   Running                          19h
```

Delete the Machine object:

```
[root@test1001 ~]# oc delete machine -n openshift-machine-api   test-day2-1-6qv96-master-0
machine.machine.openshift.io "test-day2-1-6qv96-master-0" deleted
```

Confirm the Node object got removed:

```
[root@test1001 tmp]# oc get nodes
NAME            STATUS   ROLES    AGE   VERSION
extraworker-1   Ready    worker   19h   v1.24.0+3882f8f
extraworker-2   Ready    master   20h   v1.24.0+3882f8f
extraworker-3   Ready    master   19h   v1.24.0+3882f8f
extraworker-4   Ready    worker   19h   v1.24.0+3882f8f
extraworker-5   Ready    master   15h   v1.24.0+3882f8f
```

### Check etcd-operator

As long as the underlying server is up, the etcd cluster will not get the member removed. You can see this by looking into logs of the etcd-operator:

```
$ oc logs -n openshift-etcd-operator etcd-operator-8668df65d-lvpjf

E0927 07:53:10.597523       1 base_controller.go:272] ClusterMemberRemovalController reconciliation failed: cannot remove member: 192.168.111.23 because it is reported as healthy but it doesn't have a machine nor a node resource
```

### Remove the physical machine

Take down the server and wait for etcd-operator to reconcile the cluster members:

```
[root@test1001 ~]# oc rsh -n openshift-etcd etcd-extraworker-2
sh-4.4# etcdctl member list -w table; etcdctl endpoint health
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| 61e2a86084aafa62 | started | extraworker-2 | https://192.168.111.25:2380 | https://192.168.111.25:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
https://192.168.111.26:2379 is healthy: successfully committed proposal: took = 10.458132ms
https://192.168.111.25:2379 is healthy: successfully committed proposal: took = 11.047349ms
https://192.168.111.28:2379 is healthy: successfully committed proposal: took = 11.414402ms
```
