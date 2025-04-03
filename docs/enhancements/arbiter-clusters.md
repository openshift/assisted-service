---
title: arbiter-clusters
authors:
  - "@giladravid16"
creation-date: 2025-03-19
last-updated: 2025-03-19
---

# Support 2 Node + 1 Arbiter Node HA Cluster

## Summary

This enhancement describes adding support in assisted-service to install TNA
(Two Nodes + Arbiter) clusters - OpenShift clusters with control planes that
consists of at minimum 2 normal-sized nodes, and at least 1 node that can be
less powerful than the recommended node size.
Arbiter nodes will only be running critical components for maintaining 
HA (e.g etcd) to allow the arbiter node size to be as small and as low cost as
possible within reason.

## Motivation

Customers at the edge are requiring a more economical solution for HA deployments
at the edge. They can support running 2 node clusters for redundancy but would like 
the option to deploy a lower cost node as an arbiter to supply the 3 nodes for etcd quorum.

### Goals

- Allow for a TNA cluster to be installed.
- Allow adding arbiter nodes as day2 nodes to TNA clusters only.

### Non-Goals

- Moving from 2 + 1 to a conventional 3 node cluster.

## Proposal

This enhancement will include the following:
- In the API we will:
  - Add a new host_role for arbiter.
  - Add a new field for clusters named arbiter_count. The default value will
    be zero, but if it's set to a higher amount then the cluster will be
    considered a TNA cluster.
  - Add more values to the enums for cluster's hyperthreading and disk
    encryptions in order to specify that they should be enabled on arbiters.
  - Add a new feature_support_level_id for TNA.
- We will update the clusters' validations to support TNA clusters.
- We will update the clusters' transition handler to consider arbiter nodes.
- We will add arbiter nodes' minimum requirements for clusters and per operator.
- We will auto-assign the arbiter role to hosts until we have enough based
  on the arbiter_count.

### User Stories

N/A - TNA clusters a future feature in openshift, the
[openshift enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/arbiter-clusters.md#user-stories)
has user stories that are not specific for assisted-service.

### Implementation Details/Notes/Constraints

The enums for cluster's hyperthreading and disk encryption will be:
- 'none'
- 'masters'
- 'arbiters'
- 'workers'
- 'masters,arbiters'
- 'masters,workers'
- 'arbiters,workers'
- 'masters,arbiters,workers'
- 'all' which will be the same as 'masters,arbiters,workers'

When generating the ignitions for a TNA cluster, a new base ignition named
arbiter.ign will be created. If it exists we need to patch it and upload it
to s3. The list of patches arbiter.ign needs:
- Set CA cert if it was provided.
- If all the hosts in the cluster have IPv6 address then we need to add a
  NetworkManager configuration ensuring that IPv6 DHCP requests use consistent
  client identification.

Our validations will need to be updated as follows:
- A host can only be assigned the arbiter role if the cluster is TNA.
- A cluster's arbiter_count must be zero or higher.
- A cluster's number of arbiter nodes must be exactly the cluster's
  arbiter_count, in order for the cluster to be ready for installation.
- TNA clusters' can set control_plane_count to be 2.
- TNA clusters' platform must be baremetal.
- TNA clusters are expected to be TP stating from OCP 4.19.

In the cluster's transition handler, the function enoughMastersAndWorkers
needs to be updated to also check for arbiters. This is needed to correctly
decide if the cluster is in installing/finalizing.

We need to add arbiter's hardware requirements in:
- The HW_VALIDATOR_REQUIREMENTS environment variable (by default we'll take
  the worker's requirements to avoid issues mid-development).
- The HostTypeHardwareRequirementsWrapper structs used by the operators.

We will auto-assign the arbiter role to a host if all the following are true:
- The host's cluster has less arbiter nodes than the cluster's arbiter_count. 
- The host has the minimum hardware requirements for arbiter nodes.
- Either the host doesn't have the minimum hardware requirements for master
  nodes, or the host's cluster has enough master nodes.

Outside of assisted-service, we also need to do the following:
- After we update the API in assisted-services, we need to update the vendor
  directory in assisted-installer-agent.
- We need to update the function waitForMinMasterNodes in assisted-installer
  to set the min master nodes to be the minimum of 2 (the current value)
  and number of control planes (read it from the install-config) minus 1.

Until we have this feature fully implemented, we should have an environment
variable to decide if we allow TNA clusters (default value is false).

### Risks and Mitigations

N/A

## Design Details

### Open Questions

N/A

### UI Impact

- In the host discovery we need to allow setting a host's role to be arbiter.
- In the cluster details:
  - we need to allow setting the arbiter_count, with zero as the default.
  - We need to allow setting control_plane_count to 2, but then force setting
    arbiter_count to at least 1.
  - We need a checkbox for setting arbiters' disk-encryption.

### Test Plan

- Add an e2e test that will create a TNA cluster.
- Add unit tests and subsystem tests for arbiter nodes and TNA clusters.

## Drawbacks

N/A

## Alternatives

N/A
