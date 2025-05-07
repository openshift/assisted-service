---
title: cancel-day-2-host-installation
authors:
  - "@CrystalChun"
creation-date: 2025-04-30
last-updated: 2025-04-30
---

# Cancel Day-2 Host Installation 

## Summary

In Assisted-Service's Kubernetes API implementation, there is currently no way to cancel the
installation of a day-2 host. This leads to scenarios where an Agent can become stuck for a long
time if the installation fails and the timeout hasn't been reached, or if the user changes their 
mind about installing a host before the the installation finishes. This proposal aims to allow
cancellation of installation for a day-2 host. The main use-case is for Hypershift.

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
- The host should still be responsive, in other words, any hosts that are unreachable are out scope of this proposal

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

#### Detection of Cancellation

When an Agent's cluster deployment reference is removed and the host is in the middle of installation,
the controller will consider that a trigger to cancel the installation.

Currently, there is a webhook that prevents the cluster reference from being removed
when a host is in the middle of installation. The proposal for this enhancement is to remove
that webhook to unblock the removal of the cluster reference.

#### Cancelling Installation

The host should transition to a cancelled state. This will happen through the Agent controller.

#### Cancellation Stages

There are several stages a host can be in when the install is cancelled.
These are the expected outcomes during each stage:

| Cancellation Stage | Expected Post Cancellation |
|--------------------|----------------------------|
| Host is still running assisted installer agent | No reboot needed, should transition back to discovering |
| Host is/has rebooted | Assisted should not approve CSRs and Node should not be created in the spoke cluster |
| Host registered as Node to the spoke cluster | This should be considered post-installation and unbind should take care of this as usual. The Node CR should be removed and cleaned up from the spoke cluster|

#### Resetting the Host

After the installation is cancelled, the host will need to be moved back into discovering.
There are two pathways for this depending on how the host was booted.

##### Automatic "reset"

If the host has a BMH associated with it, there is already an existing flow which will remove the
detached annotation from the BMH and that will cause it to reprovision the host automatically with
the discovery image. 

Once the host has been rebooted with the discovery image, it should be "reset"
and ready to be bound to another cluster.

##### Manual "reset"

If the host is unreachable, or the host was booted manually, then the host will stay in 
unbinding-pending-user-action state.

From here, the user must take action by manually rebooting the host with the discovery ISO.

### Risks and Mitigations

#### Risk 1: Unintended consequences

Host installation cancellation is a completely new flow that could have unintended consequences.
For example, it might not be able to be cancelled during a certain part of installation, or the
host might end up in a state that's unreachable rendering it stuck.

##### Mitigation

The way to mitigate this is to fully explore and ensure all paths into and out of this state are covered.
When testing out this feature, we should test cancelling the installation at different times and phases
to ensure that cancellation is possible.

Providing a supported documented procedure to bring the host back to discovery manually should help to mitigate this too.

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

If the host becomes unreachable during installation and the user wants to cancel it, there may be no way of
even bringing this host back in the first place and would require user intervention. Documentation
will be provided to manually recover the host.

This implementation does not include early-binding hosts as it relies on removing the cluster
reference to signal cancelling installation.

Automatically bringing a host back after cancellation can only be done if the host is associated with a BMH.
Otherwise, the user will have to manually reboot the host.

## Alternatives

### Annotating the Agent to Cancel

An option that will cover both early and late binding hosts is to expose an annotation that can
be set on Agents to request installation cancellation.

Suggested Annotation: `agent.agent-install.openshift.io/cancel-install`

When the Agent controller sees this annotation and the host is in an installing stage, then it'll
act on the annotation and cancel the host's installation.

Once the install cancellation is successful or the command times out, the controller will remove the 
annotation and transition the host accordingly.

Drawbacks:
- Day-2 hosts that are known and bound automatically start installing
    - This would potentially require another method of either pausing the installation starting 
      once the host is cancelled and reset or a way for the user to signal the installation
      to start
    - This auto installation may defeat the purpose of cancelling the installation since the 
      user might want to edit something before starting installing again

### Simply Deleting the Agent

An already existing solution to this problem is to just delete the Agent CR for the host and
then rebooting it with the discovery ISO. 

This is a very manual process, and if the installation is far along enough in the host where
the Node is already registerd, then it requires even more manual intervention.
