---
title: node labeling support
authors:
- "@oamizur"
  creation-date: 2022-12-20
  last-updated: 2023-01-30
---

# Support node labeling during installation

## Summary

There was a requirement to support node roles during installation.  These roles 
are extra roles, which are used by the user and not by any Openshift components.
These labels should be applied on a node in a spoke cluster.

Although the requirement is specifically for roles, the implementation should support setting 
any label on a node in a spoke cluster, regardless if the label represents a role.

## Motivation

Currently, nodes can be assigned with a single role which can be one of two values [ **master**, **worker**].
The may want to add additional node labels that can be used as custom roles.  

Each role in OCP are associated with machine config pool (MCP).  When a node reboots itself
from the pointer ignition, it uses specific MCP that contains the specific configuration
for the role.  This MCP has the format `rendered-<role>-*` (* stands for a hash string).  So there is MCP for master 
role `rendered-master-*` and for worker `rendered-worker-*`.

When adding custom role, it can be associated with custom MCP which references specific configuration
for that role.  Setting appropriate label a node, causes the node to reboot and apply the 
MCP configuration on that node.

Setting node labels should support this flow.

### Goals
- For ZTP flow: add capability to add and update annotations on BMH which will be transferred 
to agent CR.
- For boot it yourself flow: add capability to add and update labels to agent CR after agent
  has been created.
- Transfer the labels from the agent CR to the corresponding node on the spoke cluster.

### Non goals
- Single way to set node labels before agent creation both for ZTP flow, and boot it yourself flow.

## Proposal

Additional field **nodeLabels** will be added to the spec
section of the agent CR.  This field will be a **map[string]string**  
### ZTP flow
Labels can be applied on every CR.  So there is no known limitation to add a label on agent.

For the ZTP flow, there should a pattern for transferring an annotation from the BMH to an
entry in the field **nodeLabels** of agent CR spec.
The pattern should be **"bmac.agent-install.openshift.io.node-label."** prefix of the annotation key.  
The key suffix of the annotation will be the key in the **nodeLabels** field in the agent CR. 
The value of the annotation will be the value in the **nodeLabels** map.

For example assuming we want to apply a label **node-role.kubernetes.io/infra=** on an
a node.  Then the following annotation should be added to the BMH: **bmac.agent-install.openshift.io.node-label.node-role.kubernetes.io/infra=**.

### Node labels on agent CR

If boot it yourself flow is in use, the spec field **nodeLabels**  should be set 
directly on the agent CR.

### Moving labels to nodes

#### Day 1 cluster
When installing day 1 cluster, the implementation should make use of the **nodeLabels** field
which is part of host DB record. Once the node becomes ready, the label is applied on the node.
This functionality already exists in the assisted controller which runs as a pod on the 
spoke cluster during installation.

#### Day 2 nodes
The flow can be supported only when working with Kubernetes API, since direct access to
the cluster is required.  In this case, setting node labels should be done by a 
Kuberenetes controller (probably agent controller).

In addition, for day 2 nodes we need to support setting custom manifests.  This functionality is
already supported for day 1 clusters by the assisted controller.  In order to allow adding new
MCP to an existing day 2 cluster, the user will have to apply them on the existing cluster.

### User Stories

#### Story 1

Let the user define roles specific to the business domain in addition to 
the role that is currently assigned to nodes.  For example, storage, rt, monitoring.

### Implementation Details/Notes/Constraints

In order to apply custom role on a worker node using AI the following operations should be done:
- Add node label specifying the custom role to be associated with the node. The custom role should 
be defined using the the machinsm specified in this document.
- Optional: Add custom machine config CRs specifying additional configurations that hace to be applied on 
the node with the custom role. These manifests should be applied using the manifest API that already exists
in AI.
- Add custom machine config pool which unifies the worker machine config and the additional machine config CRs defined in the 
previous step.  Additionally, this MCP should also be associated with the custom role.  As with MC,
the MCP should be defined using the manifests API.

#### ZTP flow 

When using ZTP, the annotations in the BMH are set in the following manner: 

```yaml
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: ostest-worker-0
  namespace: assisted-installer
  annotations:
    bmac.agent-install.openshift.io.node-label.node-role.kubernetes.io/infra: ""
  labels:
    infraenvs.agent-install.openshift.io: "myinfraenv"
spec:
  online: true
  bootMACAddress: 00:ec:ee:f8:5a:ba
  automatedCleaningMode: disabled
  bmc:
    address: ....
    credentialsName: bmc-secret
```

#### Boot it yourself flow

Example that can be used to patch existing CR:

```yaml
apiVersion: agent-install.openshift.io/v1beta1
kind: Agent
metadata:
  name: 51a7c23e-00d7-4b27-b2e4-053bf5ee9cf8
  namespace: assisted-spoke-cluster
spec:
  nodeLabels:
     "node-role.kubernetes.io/infra": ""

```

### Risks and Mitigations
Currently, single MCP can be active on a specific node. Assuming that multiple roles
are needed on a specific node, it implies that a role and a superset MCP need to be created. 
The MCP should contain all machine configs that are needed be applied on the node.
In addition, only single custom should be used on the node.  This role corresponds to 
the superset MCP.
## Design Details [optional]
When custom role is applied to an existing node, the node is rebooted in order to apply
the corresponding MCP.  The extra reboot is undesirable.

There is a possibility to avoid the extra reboot by modifying the worker pointer 
ignition for a specific host.  This modification will cause the node to be attached 
to the desired MCP instead of the worker MCP.  When the node label which represents 
the custom role will be applied, the node will already be attached to the desired
MCP, so no extra reboot will be needed.
Please see https://issues.redhat.com/browse/MGMT-13186 for more details.
The effort to avoid extra reboot will be done as part of another epic: https://issues.redhat.com/browse/MGMT-13544.
### Open Questions

### UI Impact

### Test Plan

## Drawbacks

## Alternatives
