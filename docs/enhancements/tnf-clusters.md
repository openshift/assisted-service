---
title: tnf-clusters
authors:
  - "@giladravid16"
creation-date: 2025-08-21
last-updated: 2025-08-21
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

### Non-Goals

- Moving from a TNF cluster to a conventional 3 node cluster.

## Proposal

This enhancement will include the following:
- In the API, we will:
    - Add a new field called `fencing_credentials` for hosts.
    - Add a new `feature_support_level_id` for TNF.
- In the kube-api we will:
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

In the kube-api the Agent will have a new field called `FencingCredentialsSecretRef`
that will reference a secret whose data will be the same as listed above for a
host's `fencing_credentials`. If that field is not set then the `fencing_credentials`
will be created from the Agent's BMH. For ZTP we will check for an annotation on the BMH
that will specify the Agent's `FencingCredentialsSecretRef`.

if the AgentClusterInstall's `ProvisionRequirements` field specifies 2 CP nodes and 0 
arbiter nodes, then we know that the cluster will be a TNF cluster. If an Agent is
bound to such a cluster then it will update its internal host's `fencing_credentials`.

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

There is a concern about storing the hosts' `fencing_credentials` since those are
the credentials to the BMCs which is sensitive data. That data is stored as plain-text
in several places which could potentially be leaked:
1. In the internal DB that assisted-service uses.
2. In the cluster's install-config.yaml and bootstrap.ign which assisted-service saves
   to an internal object storage.
3. Temporarily on the pod's filesystem while assisted-service generates the ignitions.

It was decided in the OpenShift enhancement that this potential issue is acceptable
for non-SaaS installations. It was discussed [here](https://github.com/openshift/enhancements/pull/1675#discussion_r1801569302)
and included as part of the enhancement [here](https://github.com/openshift/enhancements/blob/master/enhancements/two-node-fencing/tnf.md#assisted-installer-family-changes).

If we wanted to fix the issue completely we would need to make sure that the data
is encrypted everywhere we store it. Adding an encryption layer will probably be
complicated when dealing with existing data and would probably require a migration
operation before enabling it.

## Design Details

### Open Questions

N/A

### UI Impact

When creating a cluster and choosing 2 as the ControlPlaneCount, the UI calls it
`Two-Nodes Arbiter` and gives a note saying that clusters with 2 CP nodes must be TNA.
This should be changed since that's no longer the case.

### Test Plan

- Add an e2e test that will create a TNF cluster.
- Add unit tests and subsystem tests for TNF clusters.

## Drawbacks

The drawback of this enhancement is storing the BMCs credentials as plain-text
as mentioned above. However, it was decided that this issue is not a big deal
for non-SaaS installations.

## Alternatives

- Instead of storing the fencing credentials on the hosts, we can save them as a
  list on the cluster. This would give users a single place to configure fencing
  credentials, but they would need to also include the node's hostname for each
  one. We would then need to validate that each hostname has a corresponding node.
