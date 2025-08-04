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
    - Add a new field called `fencing_credentials` for hosts. In the kube-api, 
      we will add to the Agent a reference to a secret that will keep the same data.
    - Add a new `feature_support_level_id` for TNF.
- We will update the clusters' validations and transition handler to support TNF clusters.
- We will update the clusters' network connectivity groups to support TNF clusters.

### User Stories

N/A

### Implementation Details/Notes/Constraints

A host's `fencing_credentials` will have the following fields:
- BMC username
- BMC password
- BMC address
- Whether we want to check the BMC's certificate or not

In the kube-api the Agent will have a new field called `FencingCredentialsSecretRef`
that will reference a secret whose data will be the same as listed above for a
host's `fencing_credentials`.

Our validations will need to be updated as follows:
- TNF Clusters are clusters that have exactly 2 CP nodes with fencing credentials.
  TNF clusters are not allowed to have arbiter nodes.
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
