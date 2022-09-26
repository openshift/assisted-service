# Installing control plane (master node) as day 2 operation

This page describes a process of adding a control plane node (master) to the OpenShift cluster that has already been installed. Depending on the version and state of the cluster, different guide applies.

Differences come from multiple facts:

* etcd versions used by different versions of OpenShift
* healthy cluster can be managed by etcd-operator, whereas an unhealthy one needs additional set of manual operations

It is important to highlight that Assisted Service only enables downloading ignition for the master node, but is not implementing any of the logic related to managing the etcd cluster itself. Therefore, an upstream OpenShift guide should always be used as the authoritary source. The guides below are only meant to provide a fast and streamlined description of the process.

## OpenShift 4.11 and newer

### Healthy cluster

If you have an access to a healthy cluster with 3 master nodes and you want to replace one of them, please use the guide linked [here](411-healthy.md). It will guide you through adding a new node as a 4th master and later through the removal of one of the existing masters.

### Unhealthy cluster

If you have access to the cluster that has only 2 healthy master nodes and you want to replace an unhealthy node, please use the guide linked [here](411-unhealthy.md). It will guide you through replacing the unhealthy master.
