---
title: 4-nodes-cluster-creation
authors:
  - "@flaper87"
creation-date: 2022-04-14
last-updated: yyyy-mm-dd
---

# Allow for a 4-nodes cluster to be created

# Summary

**Note:** Throughout this enhancement, a 4-nodes cluster refers to a 3 schedulable masters and 1 worker.

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

- Implement logic that will decide, for the user, what the best architecture is. Having a 3+1
cluster will require that masters are explicitly set to schedulable and the manifests to patch the
Infrastructure CR are provided.

- Expose this through SaaS at this stage. This would require changes in the UI and other validations
that may not worth the effort at this stage.

## Proposal

The biggest changes anticipated for this enhancement are:

- Expose the schedulable masters through the AgentClusterInstall to allow for explicitly creating
schedulable masters.

- Modify the `enoughMastersAndWorkers` validation so that it allows 1 worker when masters are
schedulable. [PoC here](https://github.com/flaper87/assisted-service/commit/f07fc6589e4f2d316266ac5533d76bd2faf471d7)
and [here](https://github.com/flaper87/assisted-service/commit/bb2d82eaacaee7790d2f18711e2ba3a442a4967d)

- Modify Assisted Service's manifests logic to allow for partial manifests to be provided. The need
for this change is twofold:
    1. This is needed because Openshift installer generates whole manifests, some of which have values that are computed at run time, which
are unknown to the user when triggering the deployment.
    2. When providing manifests overrides, OpenShift installer applies them in sequence and it
    expects entire manifests to be provided. You can think of it as the installer running `oc apply
    -f` for every yaml that exists in the manifests dir.

    Because of the above, users that want to customize manifests don't have a way to provide a
    partial manifest. Providing a full manifest may cause the override of fields computed at runtime
    by the installer.

    Here's an [example from a PoC](https://github.com/flaper87/assisted-service/commit/c6d72827b43786f95e68e052623e8aef0d4b4c0f).

### User Stories

#### Story 1

As an Infrastructure Admin, I want to be able to deploy a 4 nodes cluster as a single operation the
same way I deploy a full cluster. Aware of the risks and downsides, I want to be able to still
pursue this deployment as it fits best the hardware I have available.

### Implementation Details/Notes/Constraints [optional]

The current HTTP API allows to push full manifests. That is, a manifest that can be fed into `oc
apply -f`. As part of this work, we need to have a way to add partial manifests and, for this, we
need to decide whether we want to accept yaml-patch files or just regular yaml files. Here are a
couple of examples:

Partially populated manifest to overwrite the `status.infrastructureTopology` field:

```yaml
status:
  infrastructureTopology: HighlyAvailable
```

yaml-patch version for the above:

```yaml
- op: add
  path: /status/infrastructureTopology
  value: HighlyAvailable
```

In the first case, the current API can be re-used. Assisted service would be changed to not overwrite
files but rather merge the manifests. This is how [this PoC was
implemented](https://github.com/flaper87/assisted-service/commit/c6d72827b43786f95e68e052623e8aef0d4b4c0f).
In the `yaml-patch` case, we may want to make the API accept both, full manifest, and a `yaml-patch`
version. The API would have to allow for the user to specify, explicitly, whether the file submitted
is a `yaml-patch` or not. Note that all this would also have to be exposed through the KubeAPI.


### Risks and Mitigations

The 3+1 topology is not part of OCP's test matrix. This, to some extent, makes it a
non-supported topology. However, it has been accepted as a valid topology for the ZTPFW use-case.

The reason for the above is that a 4-nodes cluster that uses masters for infrastructure workload
would be sending infrastructure traffic to the master nodes. This, depending on the usage of the
cluster, may result in master nodes being overloaded and, in some cases, even unavailable if the
load is too high.

Using a 4-node cluster must be a conscious decision and it requires an understanding of what
workloads will be run on the cluster. This topology is not much different to a compact cluster where
there are only 3 masters. By exposing these options through the KubeAPI we will be allowing users to
create such topologies.

Finally, by exposing these options, we would also allow users to overwrite any manifest through the
KubeAPI, which may result in undefined and untested scenarios.

## Design Details [optional]

### Open Questions

N/A

### UI Impact

We won't be considering SaaS in this phase. The goal is to first expose this through KubeAPI, and
then think how to expose it on the cloud service with a better UX rather than just requesting
manifests to be pushed.

### Test Plan

- Add a basic ZTP job that deploys a 4-nodes cluster. Alternatively, we could modify the existing
job and have it always deploy 4 nodes, which would cover multiple scenarios at once.

## Drawbacks

N/A

## Alternatives

- Don't expose the manifests and just make the decision for the user. Openshift Installer, as well
as assisted service, make similar decisions when there are just 3 masters. Masters, when there are
no workers, are automatically set to schedulable. Similar behavior could be implemented for a 3+1
architecture where, in addition to setting masters as schedulable, assisted would also set the
`infrastructureTopology` to `HighlyAvailable`.
