---
title: bmac-reboot-hosts-on-iso-change
authors:
  - "@mhrivnak"
  - "@avishayt"
  - "@flaper87"
creation-date: 2021-06-30
last-updated: 2021-07-01
---

# BMAC reboots only those hosts impacted by an ISO change

## Summary

When an InfraEnv's ISO gets regenerated based on a change such as adding or
changing an NMStateConfig resource, there may be only a subset of that
InfraEnv's Agents that are affected by the change. Rather than reboot all of
them with the newly-generated ISO, only those hosts that are affected by the
new or modified NMStateConfig should be rebooted.

It is assumed that in all cases, Agents that have started installing will not
be restarted due to an ISO change.

## Motivation

If there are 500 NMStateConfigs and 500 Agents all related to the same
InfraEnv, and then someone adds the 501st NMStateConfig, we do not want to
reboot all 500 Agents. Rebooting is an expensive and time-consuming operation.
We need a way to determine which Agents or hosts should be rebooted in response
to an NMStateConfig.

### Goals

- Avoid rebooting un-provisioned BareMetalHosts when it is not needed.
- Leave the door open for other platforms (include None/boot-it-yourself) to also avoid host reboots that are not needed.

### Non-Goals

-

## Proposal

The assisted-service will track whether a particular Agent was booted using an
old InfraEnv or NMStateConfig. It will ensure that any Agent that was booted
using obsolete artifacts will have:

- a label indicating that it was booted from obsolete artifacts
- an array in its Status of references to the resources that are obsolete

### assisted-service InfraEnv controller

The InfraEnv ISO URL changes when the ISO contents change. This makes it
obvious to an API user that they have booted a host from an obsolete ISO,
because the URLs won't match.

When assisted-service creates an ISO, it will capture and include in the ISO:

- the name, namespace, UID, and generation of the InfraEnv
- the name, namespace, UID, and generation of each NMStateConfig

At runtime, those resource identifiers for the InfraEnv and the NMStateConfig
it used will be passed by the agent to assisted-service and added to the
Agent's Status.

### assisted-service Agent controller

When the Agent resource gets created, its status should include the
above-described reference details for:

- The InfraEnv
- The NMStateConfig it utilized during its boot

Upon comparing those references to the resources that currently exist, if any
of the referenced resources have changed, then the controller will add a label
to the Agent resource indicating such. It will also add a reference to each
obsolete resource to an array in the Agent's Status.

### Bare Metal Agent Controller

The Bare Metal Agent Controller will watch for Agents to have that label.

If the Agent's NMStateConfig is obsolete, then it will reboot the corresponding
BareMetalHost with the latest ISO URL.

Else if the Agent's InfraEnv is obsolete, and the Agent does not have an associated
NMStateConfig, then the BMAC will reboot the host only if finds a new
NMStateConfig that matches the Agent.

The BMAC will also watch for the InfraEnv URL to change, and if there are any
BareMetalHosts that were booted with an obsolete URL but do not yet have an
Agent resource, those BareMetalHosts will be rebooted with the latest ISO URL.

+---------------------------+
| |
| Agent has the label? |
| |
+----+-----------+----------+
| |
v v
+----+--+ +---+---+ +-------------+
| | | | | |
| Yes | | No +----->+ No action |
| | | | | |
+---+---+ +-------+ +-------------+
|
v
+---+----------------+
| |
| Agent Status |
| cites an obsolete |
| NMStateConfig? |
| |
+---+------------+---+
| |
v v
+---+---+ +---+---+
| | | |
| Yes | | No |
| | | |
+---+---+ +---+---+
| |
| v
| +--+-------------------------+
| | |
| | Is there a NMStateConfig |
| | that matches the Agent? |
| | |
| +-----------+------------+---+
| | |
| v v
+----v-----+ +---+---+ +---+---+ +-------------+
| | | | | | | |
| Reboot +<----------+ Yes | | No +---->+ No action |
| | | | | | | |
+----------+ +-------+ +-------+ +-------------+

### User Stories

#### Story 1

As a user of Zero Touch Provisioning, when I add and modify NMStateConfig
resources, unaffected hosts will not reboot. Only those hosts directly impacted
will be rebooted with a current ISO URL.

#### Story 2

As a boot-it-yourself user, I will manually or with automation watch for Agents
to have the obsolete artifact label and restart them with a fresh ISO.

### Implementation Details/Notes/Constraints [optional]

- Any Agent that has already started provisioning will be exempt from rebooting.
- Similar to current behavior, a new ISO URL will not be used until a quiet
  period has elapsed (currently 1 minute). This ensures that if multiple
  changes are happening in serial, for example many NMStateConfig resources are
  getting created or modified, that there will not be continuous churn of
  rebooting hosts with each change.

#### Label Algorithm

Given:

- The Agent's Status includes a reference to the InfraEnv resource that was used to create its ISO.
- The Agent's Status includes an optional reference to a NMStateConfig resource that was used to create the network config the agent used.

IF:

- The existing InfraEnv generation or UID don't match the corresponding values reported in the Agent's Status.

OR

- The existing NMStateConfig generation or UID don't match the corresponding values reported in the Agent's Status, or the NMStateConfig does not exist.

THEN

- Ensure that a label exists on the Agent signifying that it was booted using obsolete artifacts.
- Ensure that the obsolete InfraEnv and/or NMStateConfig are listed in an array of obsolete resource references in the Agent status.

ELSE

- Ensure that a label **does not exist** on the Agent signifying that it was booted using obsolete artifacts.
- Ensure that the Agent Status does not have an array of references to obsolete resources.

### Risks and Mitigations

## Design Details [optional]

### Open Questions

- What should the label's key and value be?
- How exactly would the resource references get embedded into the ISO, and how
  would the Agent communicate those back to assisted-service?

### UI Impact

### Test Plan

Test Cases:

- Create InfraEnv, add NMStateConfigs one by one.
- Create InfraEnv, add invalid NMStateConfig, fix NMStateConfig
- Update NMStateConfig for existing Agent
- Have multiple NMStateConfigs for single Agent
- Remove NMStateConfig for existing Agent
- Add NMStateConfig after Agent is already up
- Create InfraEnv and NMStateConfig, wait for the Agent to be up, then update proxy in InfraEnv

## Drawbacks

## Alternatives

Each NMStateConfig resource could have a unique ISO URL. The InfraEnv would
provide a base URL, and the NMStateConfig could be identified by query
parameter or similar. This way, each host would get a unique URL and a unique
ISO. When a particular NMStateConfig gets added or modified, it would
implicitly only affect any BMH with that NMStateConfig's specific URL.

Then BMAC would have to determine the correct NMStateConfig for a BareMetalHost
in advance and assign the correct URL to the BMH.

This option is more complicated, especially from an API perspective, than the proposal.
