---
title: tnf-clusters
authors:
  - "@giladravid16"
creation-date: 2025-07-21
last-updated: 2025-07-21
---

# Support Two Node OpenShift with Fencing (TNF)

## Summary

This enhancement describes adding support in assisted-service to install TNF
(Two Nodes with Fencing) clusters - OpenShift clusters with only 2 control plane
nodes that use their BMCs to configure high availability. See the OCP enhancement
[here](https://github.com/openshift/enhancements/blob/master/enhancements/two-node-fencing/tnf.md).

## Motivation

Customers at the edge are requiring a more economical solution for HA deployments.
TNF clusters only require 2 nodes, so clusters can be kept at a low cost.

### Goals

- Allow for a TNF cluster to be installed.
- Track usage of TNF clusters in metrics.

### Non-Goals

- Moving from a TNF cluster to a conventional 3 node cluster.

## Proposal

This enhancement will include the following:
- In the API, we will:
    - Add a new boolean field called `use_fencing` for clusters with a default value of false.
    - Add a new optional parameter to `V2InstallCluster` for install-config overrides
      similar to the field `install_config_overrides` in clusters.
    - Add a new `feature_support_level_id` for TNF.
- In the kube-api we will:
  - Add a new boolean field for AgentClusterInstall called `use_fencing` with a
    default value of false.
  - Add a new field for Agents called `FencingCredentialsSecretRef`.
  - Add a new annotation for a BMH to specify an Agent's `FencingCredentialsSecretRef`.
- We will update the clusters' validations and transition handler to support TNF clusters.
- We will update the clusters' network connectivity groups to support TNF clusters.

### User Stories

N/A

### Implementation Details/Notes/Constraints

In order to install a TNF cluster we need to specify `fencing_credentials`
for the 2 CP nodes in the install-config.
A host's `fencing_credentials` will have the following fields:
- BMC username
- BMC password
- BMC address
- Whether we want to check the BMC's certificate or not

We decided we don't want to store the hosts' `fencing_credentials` in our DB.
But we still need to pass on that information to the install-config, and we
need to make sure that TNF clusters pass their validations without the credentials.
We accomplish this with 2 things:
1. The cluster object will hold a boolean that will hold whether the cluster will be TNF.
   This will be used in order to correctly calculate the validation results. 
2. The endpoint `V2InstallCluster` will accept `fencing_credentials` through a new general
   purpose field for install-config overrides. The credentials won't be saved on our DB,
   and we will pass them to the install-config.

In the kube-api the Agent will have a new field called `FencingCredentialsSecretRef`
that will reference a secret whose data will be the same as listed above for a
host's `fencing_credentials`. If that field is not set then the `fencing_credentials`
will be created from the Agent's BMH. For ZTP we will check for an annotation on the BMH
that will specify the Agent's `FencingCredentialsSecretRef`.

The AgentClusterInstall will have a new boolean field called `use_fencing` with a default
value of false. If the value is true then when trying to install the cluster, the cluster
deployment controller will collect the `fencing_credentials` of the Agent and define the
`install_config_overrides` which will be sent to `V2InstallCluster`.

Our validations will need to be updated as follows:
- TNF Clusters must have exactly 2 CP nodes and no arbiter nodes.
- TNF clusters' platform must be either baremetal or none.
- TNF clusters' ocp version must be at least 4.20.

In the cluster's transition handler, the function `enoughMastersAndWorkers`
needs to be updated to also check if the cluster is TNF.

Currently, when we create network connectivity groups, we assume that every non-SNO
cluster has at least 3 nodes. But TNF clusters only have 2, so when we create the
majority groups, we need to pass an argument for the minimum number of hosts.
By default, it will be 3, but for TNF clusters it will be 2.

TNF clusters won't be supported in the SaaS offering, so we need to have an
environment variable to decide if we allow TNF clusters (default value is false).

### Risks and Mitigations

N/A

## Design Details

### Open Questions

- Should we add an option in ACM's UI to edit the fencing credentials?

### UI Impact

When creating a cluster and choosing 2 as the ControlPlaneCount, the UI calls it
`Two-Nodes Arbiter` and gives a note saying that clusters with 2 CP nodes must be TNA.
This should be changed since that's no longer the case.

### Test Plan

- Add an e2e test that will create a TNF cluster.
- Add unit tests and subsystem tests for TNF clusters.

## Drawbacks

N/A

## Alternatives

- Instead of storing the fencing credentials on the hosts, we can save them as a
  list on the cluster. This would give users a single place to configure fencing
  credentials, but they would need to also include the node's hostname for each
  one. We would then need to validate that each hostname has a corresponding node.
