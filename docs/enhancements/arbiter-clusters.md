---
title: arbiter-clusters
authors:
  - "@giladravid16"
creation-date: 2025-03-13
last-updated: 2025-03-13
---

# Support 2 Node + 1 Arbiter Node HA Cluster

## Summary

This enhancement describes the ability to install TNA (Two Nodes + Arbiter)
clusters - OpenShift clusters with control planes that consists of at minimum
2 normal-sized nodes, and at least 1 node that can be less powerful than the
recommended node size.
Arbiter nodes will only be running critical components for maintaining 
HA (e.g etcd) to allow the arbiter node size to be as small and as low cost as
possible within reason.

## Motivation

Customers at the edge are requiring a more economical solution for HA deployments
at the edge. They can support running 2 node clusters for redundancy but would like 
the option to deploy a lower cost node as an arbiter to supply the 3 nodes for etcd quorum.

### Goals

- Allow for a TNA cluster to be installed.

### Non-Goals

- Moving from 2 + 1 to a conventional 3 node cluster.

## Proposal

This enhancement will include the following:
- In the API we will:
  - Add a new host_role for arbiter.
  - Add a new high_availability_mode for TNA.
  - Deprecate the existing parameters and create new ones for hyperthreading
    and disk encryption (they will be maps of host_role to bool).
  - Add a new feature_support_level_id for TNA.
- We will update the clusters' validations and transitions for TNA clusters.
- We will add arbiter nodes' minimum requirements for clusters and per operator.
- We will auto-assign the arbiter role to a host.

### User Stories

- As a solutions architect for a retail organization, I want to deploy OpenShift
  at n number of store locations at the edge with only 2 regular sized nodes and
  1 lower cost node to maintain HA and keep compute costs down.
- As a solutions architect for cloud infrastructures, I want to offer low cost
  OpenShift deployments on purpose built hardware for a 2 + 1 configuration.
- As an OpenShift cluster admin I want non-critical applications deployed to my
  2 + 1 arbiter node cluster to not be scheduled to run on the arbiter node.

### Implementation Details/Notes/Constraints

Our validations will need to be updated as follows:
- A host can only be assigned the arbiter role if the cluster is TNA.
- TNA clusters must have at least 1 arbiter node.
- TNA clusters can have 2-5 control plane nodes.
- TNA clusters' platform must be baremetal.
- TNA clusters are expected to be TP stating from OCP 4.19.

We need to add arbiter's hardware requirements in:
- The HW_VALIDATOR_REQUIREMENTS environment variable (by default we'll take
  the worker's requirements to avoid issues mid-development).
- The HostTypeHardwareRequirementsWrapper structs used by the operators.

We will auto-assign the arbiter role to a host if the following are true:
- The host's cluster is TNA.
- The host's cluster has no arbiter nodes.
- The host has the minimum hardware requirements for arbiter nodes.

Outside of assisted-service, we also need to do the following:
- After we update the API in assisted-services, we need to update the vendor
  directory in assisted-installer-agent.
- We need to update the function ListMasterNodes in assisted-installer to
  also get arbiter nodes.

Until we have this feature fully implemented, we should have an environment
variable to decide if we allow TNA clusters (default value is false).

### Risks and Mitigations

The main risk in this enhancement is that because we are treating one of the
master nodes in a 3 node cluster as an arbiter, we are explicitly evicting
processes that would otherwise be a normal supported upstream configuration
such as a compact cluster. We run the risk of new components being critical to
HA not containing the proper tolerations for running on the arbiter node. One
of the mitigations we can take against that is to make sure we are testing
installs.

A couple of risks we run is customers using arbiter nodes with improper disk
speeds below that recommended for etcd, or a bigger problem being network
latency. Since etcd is sensitive to latency between members, we should
validate that arbiter nodes meet minimum requirements for ETCD to function
properly, in disk and network speeds.

## Design Details

### Open Questions

N/A

### UI Impact

The main change is that currently the UI relies on the user specifying the
number of control plane in order to set the high_availability_mode, but TNA
cluster can have 2-5 control planes so we should ask for it explicitly and
validate it with the number of control planes.

### Test Plan

- Add an e2e test that will create a TNA cluster.
- Add unit tests and subsystem tests for arbiter nodes and TNA clusters.

## Drawbacks

A few drawbacks we have is that we will be creating a new variant of OpenShift
that implements a new unique way of doing HA for kubernetes. This does mean
an increase in the test matrix. We will also need to keep the option of arbiter
nodes in mind for future features and operators and make sure it's supported.

## Alternatives

We originally had tried using the pre-existing features in OCP, such as setting
a node as NoSchedule to avoid customer workloads going on the arbiter node.
While this whole worked as expected, the problem we faced is that the desire
is to use a device that is lower power and is cheaper as the arbiter. This
method would still run most of the OCP overhead on the arbiter node.
