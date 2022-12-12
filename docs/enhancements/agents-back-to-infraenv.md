---
title: agents-back-to-infraenv
authors:
  - "@rollandf"
creation-date: 2021-10-25
last-updated: 2021-11-03
---

# Return Agents back to InfraEnv - late binding

## Summary

When a user deletes a ClusterDeployment/AgentClusterInstall resource, the Agents resources bound to that CD should not be deleted.
In that case, the Agents should be unbound and if needed the host should be rebooted with the Discovery ISO.

In addition, the user should be able to unbind a single Agent from a ClusterDeployment so that this Agent should be available back in the InfraEnv running with the Discovery ISO even if the Agent is already installed or in error/cancelled phases.

Note that this mechanism should be available only for Agents created from an InFraEnv that is not associated to a ClusterDeployment.

## Motivation

In the late binding flow, the cluster creator selects the Agents he wants to include in a new cluster from a collection of free Agents managed by the infrastructure administrator.

Once the cluster creator deletes the cluster or unbinds a specific Agent, the Agents should be available back to the collection of free Agents for use in future clusters.

### Goals

- Return Agents to the InfraEnv after the CD/ACI they are bound to is deleted.
- Return Agents to the InfraEnv after the Agent is unbound even if the Agent is already installed.
- If needed, the host should be rebooted with the Discovery ISO.

### Non-Goals

- Graceful removal of nodes from an installed OpenShift cluster is out of scope of this proposal.
- Unbinding a single Agent that is in installation phases is not supported for now.

## Proposal

Once a user deletes a CD/ACI created with late binding, the assisted-service will not delete the Agents/Hosts.
It will remove the CD reference from the Agents and in case that it needs to be rebooted, the host will move to a new state `unbinding-requires-user-action`.

If the user unbinds a single Agent that is already installed or in error/cancelled phases, the host will move to the state `unbinding-requires-user-action`.

The host will move to the state `unbinding-requires-user-action` if it was in one of the following state: `Installed`, `Cancelled` , `Error` or `Added To Existing Cluster`.

In case of Zero Touch Provisioning, the Bare Metal Agent Controller will detect the state and it will reboot the corresponding BareMetalHost with the Discovery ISO.

In case of Boot It YourSelf, it is the user responsibility to reboot the host.

### assisted-service

When a cluster is deleted:

- For each of the hosts:
  - If the Host's InfraEnv is associated to the Cluster (not late binding):
    - Delete the Host
  - If the Host's InfraEnv is not associated to the Cluster (late binding):
    - Unbind the Host.

### assisted-service ClusterDeployment controller

When a ClusterDeployment/AgentClusterInstall is deleted:

- For each of the agents:
  - If the Host's InfraEnv is associated to the Cluster (not late binding):
    - Delete the Agent CR
  - If the Host's InfraEnv is not associated to the Cluster (late binding):
    - Unbind by updating the Agent CR CD's reference to nil.

### Agent Controller

Support new `Reason` for `Unbound` condition in case the host is in `unbinding-requires-user-action` state.

### assisted-service host state machine

When `UnBindHost` is called, move to the state of `unbinding-requires-user-action` if the current state is in one of the following states: `Installed`, `Cancelled` , `Error` or `Added To Existing Cluster`.

### Bare Metal Agent Controller

The Bare Metal Agent Controller will watch for Agents and monitor the `Bound` condition.
If the Reason is `unbinding-requires-user-action`, the BareMetalHost will be rebooted with the Discovery ISO.

### User Stories

#### Story 1

As an Infrastructure Admin, I want that hosts that are not used anymore by the Cluster Creator to be available back as unbound Agents in the original InfraEnv.

#### Story 2

As a Cluster Creator, I want to be able to delete a ClusterDeployment with Agents so that these Agents will be available to create
a new Cluster.

#### Story 3

As a Cluster Creator, I want to be able to unbind an Agent from a ClusterDeployment so that this Agent will be available to use in a new Cluster even if the Agent is already installed or in error/cancelled stages.

### Implementation Details/Notes/Constraints

- On Boot It Yourself flow, it is the user responsibility to reboot the host and remove the need-boot label.

### Risks and Mitigations

## Design Details [optional]

### Open Questions

- Should we support in the future to Unbind a single Host that is in installation phases?
- If the installation already started, should there be a mechanism to signal the Agent to stop the installation and register again? (Optimization path, instead of rebooting the host if Agent is still running)

### UI Impact

There should be no impact for UI in Central Management Infrastructure flows.

### Test Plan

Test Cases:

- Create a cluster with Agents not using late-binding
  - Delete the cluster. Agents should be deleted.
- Create a cluster with Agents using late-binding
  - Delete the cluster. Agents should not be deleted.
  - The Agents should not have a reference to the CD.
  - `Bound` condition should be if `false` with reason `UnbindingPendingUserAction` if the agents were in `Installed`, `Cancelled`, `Error` or `Added To Existing Cluster`.
  - If BMH is used, the host should be rebooted.
- Install a cluster with Agents using late-binding
  - Unbind an installed Agent
  - `Bound` condition should be if `false` with reason `UnbindingPendingUserAction` if the agents were in `Installed`, `Cancelled`, `Error` or `Added To Existing Cluster`.
  - If BMH is used, the host should be rebooted.

## Drawbacks

## Alternatives

Instead of the keeping the Agent, the controller can delete it, and mark the BMH to reboot from ISO.
The Agent will be recreated after the reboot of the host.

In this case, the spec of the Agent will be lost (Hostname, Role, Installation Disk) and all the hosts will be rebooted regardless of the Agent state.

Note, that having the Agent CR provides easier debug source for the user.
