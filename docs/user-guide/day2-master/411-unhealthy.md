# Adding master node on OpenShift 4.11 (and newer) with 2 healthy and 1 unhealthy node

## Confirm initial state of the cluster

Master node should be detected as unhealthy:

```
[root@test1001 tmp]# oc get nodes
NAME            STATUS     ROLES    AGE   VERSION
extraworker-1   Ready      worker   20h   v1.24.0+3882f8f
extraworker-2   NotReady   master   20h   v1.24.0+3882f8f
extraworker-3   Ready      master   20h   v1.24.0+3882f8f
extraworker-4   Ready      worker   20h   v1.24.0+3882f8f
extraworker-5   Ready      master   15h   v1.24.0+3882f8f
```

Etcd-operator should detect cluster as unhealthy:

```
$ oc logs -n openshift-etcd-operator etcd-operator-8668df65d-lvpjf

E0927 08:24:23.983733       1 base_controller.go:272] DefragController reconciliation failed: cluster is unhealthy: 2 of 3 members are available, extraworker-2 is unhealthy
```

Etcd should show an unhealthy member of the cluster. The cluster itself should be marked as unhealthy:

```
[root@test1001 ~]# oc rsh -n openshift-etcd etcd-extraworker-3
sh-4.4# etcdctl member list -w table; etcdctl endpoint health
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| 61e2a86084aafa62 | started | extraworker-2 | https://192.168.111.25:2380 | https://192.168.111.25:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
{"level":"warn","ts":"2022-09-27T08:25:35.953Z","logger":"client","caller":"v3/retry_interceptor.go:62","msg":"retrying of unary invoker failed","target":"etcd-endpoints://0xc000680380/192.168.111.25:2379","attempt":0,"error":"rpc error: code = DeadlineExceeded desc = latest balancer error: last connection error: connection error: desc = \"transport: Error while dialing dial tcp 192.168.111.25:2379: connect: no route to host\""}
https://192.168.111.28:2379 is healthy: successfully committed proposal: took = 12.465641ms
https://192.168.111.26:2379 is healthy: successfully committed proposal: took = 12.297059ms
https://192.168.111.25:2379 is unhealthy: failed to commit proposal: context deadline exceeded
Error: unhealthy cluster
```

## Remove the unhealthy master

Delete the Machine object:

```
[root@test1001 tmp]# oc delete machine -n openshift-machine-api test-day2-1-6qv96-master-2
machine.machine.openshift.io "test-day2-1-6qv96-master-2" deleted
```

Check if the Node object got removed. If you are unlucky and your unhealthy cluster will not be able to run the finalizers correctly, the Machine object will never get deleted. In this case also the Node object will not be removed.

### Check etcd-operator

Etcd-operator will not reconcile an unhealthy cluster. This is the case as we have a cluster that has only 2 healthy master nodes. You can confirm that etcd-operator is not removing the unhealthy machine as below:

```
[root@test1001 ~]# oc logs -n openshift-etcd-operator etcd-operator-8668df65d-lvpjf -f

I0927 08:58:41.249222       1 machinedeletionhooks.go:135] skip removing the deletion hook from machine test-day2-1-6qv96-master-2 since its member is still present with any of: [{InternalIP } {InternalIP 192.168.111.26}]
```

Remove the unhealthy etcd member manually:

```
[root@test1001 master2]# oc rsh -n openshift-etcd etcd-extraworker-3

sh-4.4# etcdctl member list -w table
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| 61e2a86084aafa62 | started | extraworker-2 | https://192.168.111.25:2380 | https://192.168.111.25:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+

sh-4.4# etcdctl endpoint health
{"level":"warn","ts":"2022-09-27T10:31:07.227Z","logger":"client","caller":"v3/retry_interceptor.go:62","msg":"retrying of unary invoker failed","target":"etcd-endpoints://0xc0000d6e00/192.168.111.25:2379","attempt":0,"error":"rpc error: code = DeadlineExceeded desc = latest balancer error: last connection error: connection error: desc = \"transport: Error while dialing dial tcp 192.168.111.25:2379: connect: no route to host\""}
https://192.168.111.28:2379 is healthy: successfully committed proposal: took = 13.038278ms
https://192.168.111.26:2379 is healthy: successfully committed proposal: took = 12.950355ms
https://192.168.111.25:2379 is unhealthy: failed to commit proposal: context deadline exceeded
Error: unhealthy cluster

sh-4.4# etcdctl member remove 61e2a86084aafa62
Member 61e2a86084aafa62 removed from cluster 6881c977b97990d7

sh-4.4# etcdctl member list -w table
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
```

## Add day-2 master

Please follow the pre-existing guide describing the process of adding a day 2 node. Please note that you need to annotate the agent with `role: master`.

A sample guide for adding a day-2 node is available [here](../rest-api-day2.md).

### Accept CSRs

During the installation there may be some CSRs to approve:

```
[root@test1001 ~]# oc get csr | grep Pending
csr-5sd59   8m19s   kubernetes.io/kube-apiserver-client-kubelet   system:serviceaccount:openshift-machine-config-operator:node-bootstrapper   <none>              Pending
```

```
[root@test1001 ~]# oc get csr | grep Pending
csr-xzqts   10s     kubernetes.io/kubelet-serving                 system:node:extraworker-6                                                   <none>              Pending
```

### Confirm the new master node is ready

```
[root@test1001 ~]# oc get nodes
NAME            STATUS   ROLES    AGE     VERSION
extraworker-1   Ready    worker   22h     v1.24.0+3882f8f
extraworker-3   Ready    master   22h     v1.24.0+3882f8f
extraworker-4   Ready    worker   22h     v1.24.0+3882f8f
extraworker-5   Ready    master   17h     v1.24.0+3882f8f
extraworker-6   Ready    master   2m52s   v1.24.0+3882f8f
```

## Ensure correct Machine, Node and BareMetalHost CRs (only if running with Machine API)

If the cluster is running with [functional Machine API](https://github.com/openshift/enhancements/blob/master/enhancements/etcd/protecting-etcd-quorum-during-control-plane-scaling.md#when-is-machine-api-functional), etcd-operator requires Machine objects to be present. As those may not always be created as part of the day-2 process, we need to make sure those exist. The simplest way to distinguish between functional and non-functional Machine API is to look for presence or absence of Machines in the `Running` phase.

### Create Machine object linked with BMH and Node

Make sure there is a Machine object referencing the newly added node. If you are deploying using e.g. Zero Touch Provisioning, this step may not be needed. Boot-it-yourself will not create BareMetalHost and Machine so you need to make sure they exist. Otherwise etcd-operator will not do its job correctly.

### Add BareMetalHost object

[...]

### Add Machine object

[...]

### Link BMH and Machine and Node using the magic script

[...]

```
# bash link-machine-and-node.sh custom-master4-chocobomb extraworker-6
```

## Confirm members of etcd

Execute in the scope of one of the old masters:

```
[root@test1001 master2]# oc rsh -n openshift-etcd etcd-extraworker-3
sh-4.4# etcdctl member list -w table
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
|        ID        | STATUS  |     NAME      |         PEER ADDRS          |        CLIENT ADDRS         | IS LEARNER |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
| 2c18942ff301e223 | started | extraworker-3 | https://192.168.111.26:2380 | https://192.168.111.26:2379 |      false |
| 79153c5a1a890c03 | started | extraworker-6 | https://192.168.111.29:2380 | https://192.168.111.29:2379 |      false |
| ead4f28057169c15 | started | extraworker-5 | https://192.168.111.28:2380 | https://192.168.111.28:2379 |      false |
+------------------+---------+---------------+-----------------------------+-----------------------------+------------+
```

## Wait for etcd-operator

It takes a while for the operator to rollout configuration to all the nodes. You can monitor the process as below:

```
[root@test1001 master2]# oc get clusteroperator etcd
NAME   VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
etcd   4.11.5    True        True          False      22h     NodeInstallerProgressing: 1 nodes are at revision 17; 1 nodes are at revision 20; 1 nodes are at revision 22
```

After finished it should look like below:

```
[root@test1001 master2]# oc get clusteroperator etcd
NAME   VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
etcd   4.11.5    True        False         False      22h
```

## Confirm health of the etcd

```
[root@test1001 master2]# oc rsh -n openshift-etcd etcd-extraworker-3
sh-4.4# etcdctl endpoint health
https://192.168.111.26:2379 is healthy: successfully committed proposal: took = 9.105375ms
https://192.168.111.28:2379 is healthy: successfully committed proposal: took = 9.15205ms
https://192.168.111.29:2379 is healthy: successfully committed proposal: took = 10.277577ms
```

## Confirm health of the cluster

```
[root@test1001 master2]# oc get Nodes
NAME            STATUS   ROLES    AGE   VERSION
extraworker-1   Ready    worker   22h   v1.24.0+3882f8f
extraworker-3   Ready    master   22h   v1.24.0+3882f8f
extraworker-4   Ready    worker   22h   v1.24.0+3882f8f
extraworker-5   Ready    master   18h   v1.24.0+3882f8f
extraworker-6   Ready    master   40m   v1.24.0+3882f8f
```

```
[root@test1001 master2]# oc get ClusterOperators
NAME                                       VERSION   AVAILABLE   PROGRESSING   DEGRADED   SINCE   MESSAGE
authentication                             4.11.5    True        False         False      150m
baremetal                                  4.11.5    True        False         False      22h
cloud-controller-manager                   4.11.5    True        False         False      22h
cloud-credential                           4.11.5    True        False         False      22h
cluster-autoscaler                         4.11.5    True        False         False      22h
config-operator                            4.11.5    True        False         False      22h
console                                    4.11.5    True        False         False      145m
csi-snapshot-controller                    4.11.5    True        False         False      22h
dns                                        4.11.5    True        False         False      22h
etcd                                       4.11.5    True        False         False      22h
image-registry                             4.11.5    True        False         False      22h
ingress                                    4.11.5    True        False         False      22h
insights                                   4.11.5    True        False         False      22h
kube-apiserver                             4.11.5    True        False         False      22h
kube-controller-manager                    4.11.5    True        False         False      22h
kube-scheduler                             4.11.5    True        False         False      22h
kube-storage-version-migrator              4.11.5    True        False         False      148m
machine-api                                4.11.5    True        False         False      22h
machine-approver                           4.11.5    True        False         False      22h
machine-config                             4.11.5    True        False         False      110m
marketplace                                4.11.5    True        False         False      22h
monitoring                                 4.11.5    True        False         False      22h
network                                    4.11.5    True        False         False      22h
node-tuning                                4.11.5    True        False         False      22h
openshift-apiserver                        4.11.5    True        False         False      163m
openshift-controller-manager               4.11.5    True        False         False      22h
openshift-samples                          4.11.5    True        False         False      22h
operator-lifecycle-manager                 4.11.5    True        False         False      22h
operator-lifecycle-manager-catalog         4.11.5    True        False         False      22h
operator-lifecycle-manager-packageserver   4.11.5    True        False         False      22h
service-ca                                 4.11.5    True        False         False      22h
storage                                    4.11.5    True        False         False      22h
```

```
[root@test1001 master2]# oc get ClusterVersion
NAME      VERSION   AVAILABLE   PROGRESSING   SINCE   STATUS
version   4.11.5    True        False         22h     Cluster version is 4.11.5
```
