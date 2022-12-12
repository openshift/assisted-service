---
title: 4-nodes-cluster-creation
authors:
  - "@flaper87"
creation-date: 2022-04-14
last-updated: yyyy-mm-dd
---

# Allow for a 4-nodes cluster to be created

# Summary

**Note:** Throughout this enhancement, a 4-nodes cluster refers to a 3 schedulable masters and 1 worker cluster.

There are cases where customers may just have 4 nodes available and no chance to add more. This was
one of the cases in the ZTP for Factory Workflow where it was required to deploy a cluster in an
enclosure that only had 4 nodes. The goal was to use all of the blades rather than just 3 of them.

For now, this is achieved with a 2 step deployment, first 3 masters and then add a worker node. It
would be more time efficient as well as easier and more reliable to be able to do this deployment in
one step, day0.

# Motivation

Assisted service has built-in validations that prevent the deployment of a 4-nodes cluster. This is
to prevent the deployment of topologies that are not part of the OCP standard test matrix. A 4-nodes
cluster doesn't take schedulable masters into consideration when there are workers being deployed,
which causes the installer to chose an `InfrastructureTopology` that is SingleReplica. This means
that infrastructure workloads will run as single replicas, only on workers. This setting cannot be
changed once the cluster is deployed.

The above was brought up to the OpenShift team as [an enhancement](https://github.com/openshift/enhancements/pull/1057/). The feedback so far is that
it's OK for users (or other installers) to overwrite the openshift manifests to achieve the
deployment topologies required. The conversation is still in progress as far as understanding if
some changes could be done to the openshift installer to improve the user experience here but we
were encouraged to pursue manifests' overwrite as a way to provide this through assisted service.

### Goals

- Allow for a 4-nodes cluster to be created using the KubeAPI flow

### Non-Goals

- Expose this through SaaS at this stage. This would require changes in the UI and other validations
  that may not worth the effort at this stage.

## Proposal

The biggest changes anticipated for this enhancement are:

- Modify Assisted Service so that 3 nodes will be set as schedulable masters and 1 as worker, whenever a 4-node cluster
  is created.

- Modify the `enoughMastersAndWorkers` validation so that it allows 1 worker when masters are
  schedulable. [PoC here](https://github.com/flaper87/assisted-service/commit/f07fc6589e4f2d316266ac5533d76bd2faf471d7)
  and [here](https://github.com/flaper87/assisted-service/commit/bb2d82eaacaee7790d2f18711e2ba3a442a4967d)

- Modify Assisted Installer so that it will automatically set the `InfrastructureTopology` to `HighlyAvailable` in the
  `cluster-infrastructure-02-config.yml` manifest, whenever a 4 nodes cluster is requested.

### User Stories

#### Story 1

As an Infrastructure Admin, I want to be able to deploy a 4 nodes cluster as a single operation the
same way I deploy a full cluster. Aware of the risks and downsides, I want to be able to still
pursue this deployment as it fits best the hardware I have available.

### Implementation Details/Notes/Constraints [optional]

Assisted Service will only allow for a single configuration when deploying a 4-nodes cluster: 3
nodes will be set as schedulable masters, and 1 as worker. The InfrastructureTopology will be
HighlyAvailable. This configuration provides a deployment that is easy to scale out or in, without
affecting the health of the cluster. All this will happen automatically, no input or extra
configuration from the user will be required.

### Risks and Mitigations

The 3+1 topology is not part of OCP's test matrix. This, to some extent, makes it a
non-supported topology. However, it has been accepted as a valid topology for the ZTPFW use-case.

The reason for the above is that a 4-nodes cluster that uses masters for infrastructure workload
would be sending infrastructure traffic to the master nodes. This, depending on the usage of the
cluster, may result in master nodes being overloaded and, in some cases, even unavailable if the
load is too high.

Using a 4-node cluster must be a conscious decision and it requires an understanding of what
workloads will be run on the cluster. This topology is not much different to a compact cluster where
there are only 3 masters. By exposing these deployment through the KubeAPI we will be allowing users to
create such topologies.

## Design Details [optional]

### Open Questions

N/A

### UI Impact

We won't be considering SaaS in this phase. The goal is to first expose this through KubeAPI, and
then think how to expose it on the cloud service with a better UX.

### Test Plan

- Add a basic ZTP job that deploys a 4-nodes cluster. Alternatively, we could modify the existing
  job and have it always deploy 4 nodes, which would cover multiple scenarios at once.

## Drawbacks

N/A

## Alternatives

- Request users to explicitly set the InfrastructureTopology as HighlyAvailable and not make any
  decision for them. This would open the doors for undefined behavior and it may result in broken, or
  unhealthy clusters being created. The idea in this enhancement is to start with sane, useful,
  defaults and then expand the functionality to provide more control.
