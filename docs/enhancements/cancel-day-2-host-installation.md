---
title: cancel-day-2-host-installation
authors:
  - "@CrystalChun"
creation-date: 2025-04-01
last-updated: 2025-04-01
---

# Cancel Day-2 Host Installation

## Summary

In Assisted-Service's Kubernetes API implementation, there is currently no way to cancel the
installation of a day-2 host. This leads to scenarios where an Agent can become stuck for a long
time if the installation fails and the timeout hasn't been reached, or if the user changes their 
mind about installing a host before the the installation finishes. This proposal aims to allow
cancellation of installation for a day-2 host.

## Motivation

There would be times a user is installing a day-2 host and in the middle of installation, they need to
change something, or the install becomes stuck because of an error. There are also instances where a
user might need to provision and deprovision a host quickly as it may no longer be needed, such as when
autoscaler is used. 

When a host starts installation, there's no way to remove it from a cluster until the host installation
times out and errors out or it finishes installation. These can take a very long time, and it can be a
frustrating experiences for users.

### Goals

- Once a day-2 host starts installation, the installation can be cancelled
- There's a straight-forward way to trigger cancellation
- Host whose installations are cancelled can be reclaimed back to their InfraEnvs and be reused
- Host cancellation should not affect other hosts in the cluster and should not affect the cluster itself
- Host cancellation should happen within a reasonable amount of time in order to support dynamic scaling 

### Non-Goals

- Hosts that are installed in a day-1 cluster is out of scope of this enhancement
- Cluster installation cancellation is out of scope
- The way reclaim/unbind works should not be affected by these changes
- This enhancement is focused solely on Assisted-Service offerings using Kubernetes API (SaaS should not be affected by this)

## Proposal

Allow cancelling an installing day-2 host through Assisted-Service's Kubernetes API.

### User Stories

#### Story 1

As a cluster admin, when I create a hosted cluster using Hypershift, I'd like to bind and install worker hosts into my cluster. 
However, if I want to remove the host from the cluster before they finish installing, I should be able to do so and reuse the host for another cluster.

#### Story 2

As a cluster admin using Hypershift, I want to use the autoscaler.
When the scaling happens and hosts are bound/unbound from the cluster, the hosts should bind and unbind 
easily from the cluster.

#### Story 3

As a cluster admin, if I install a day-2 host to a cluster, and I see the host installation fails from
inside the host. I should be able to cancel the installation once it starts and quickly reset the host.

### Implementation Details/Notes/Constraints [optional]

There is an already existing cancel host installation API which maps to the unbind and reclaim stages.
This is currently only being used when a day-1 cluster installation is cancelled. 

#### Late-binding case

For late-binding, an Agent can be reused by another cluster so in order to detect if the Agent
becomes unbound from a cluster while installing, we'll check if the cluster reference on the
Agent has been removed or changed. Currently, the existing Agent webhook blocks this change
if the Agent is in an installing stage. For this proposal, we can remove that webhook.

Once the Agent is detected as unbound during installation, then we can use the existing cancellation
API to cancel the host's installation.

#### Early-binding case

For early-binding, an Agent is directly attached to a cluster when it's discovered. In order to
detect cancellation, the Agent would need to be deleted while it's installing. 

Currently, the webhook allows an Agent to be deleted while it's installing. 
Once an Agent is deleted, it is simply removed from the database.

This flow would need to be modified to detect if the Agent is in the middle of installation,
and if it is, then send the cancel install host command to the Agent before completely allowing
it to be removed. The Agent should move to the cancel installation stage until the host command
returns successfully. If the command is not successful, then the Agent will move to an Error state.

There may be some cases where a user might not care if the host is cancelled if they're deleting
the Agent, so there should also be a hook that allows the host to be force deleted without waiting
for the cancel command to complete successfully.

### Risks and Mitigations

#### Risk 1: Unintended consequences

Host installation cancellation is a completely new flow that could have unintended consequences.
For example, it might not be able to be cancelled during a certain part of installation, or the
host might end up in a state that's unreachable rendering it stuck.

##### Mitigation

The way to mitigate this is to fully explore and ensure all paths into and out of this state are covered.
When testing out this feature, we should test cancelling the installation at different times and phases
to ensure that cancellation is possible.

## Design Details [optional]


### Open Questions

1. Are there limitations when cancelling an in-progress OpenShift install?

### UI Impact

The UI could expose a cancel install button if a host is being added to an imported or installed cluster. 
This cancel button should remove the cluster deployment reference from the Agent to trigger a
cancellation. The cancel button should only be exposed once the host starts installing. 

### Test Plan

An e2e test will need to be added or updated for day-2 host installation and cancellation.

## Drawbacks

There are a lot of risks when changing the host stages like this and when interrupting an installation as it can leave the host in a broken state.

If the host becomes unreachable during installation and the user wants to cancel it, there may be no way of even bringing this host back in the first place.

## Alternatives

