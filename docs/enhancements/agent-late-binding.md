---
title: agent-late-binding
authors:
  - "@avishayt"
creation-date: 2021-05-19
last-updated: 2021-05-19
---

# Agent Late Binding

## Summary

In the current implementation of Assisted Installer, each Discovery ISO is associated with an existing cluster resource (a cluster definition that may not yet be an installed OCP cluster). When an agent comes up, it registers with the Assisted Service and is automatically bound to that same cluster resource. The only way to bind that host to a different cluster is to boot it with an ISO associated with the desired cluster. We can see that the process of configuring the infrastructure is closely tied to cluster creation.

The late binding feature splits the process into two parts, each potentially performed by a different persona. The Discovery ISO is created by an infrastructure admin, who then boots the hosts. Each host becomes visible to the cluster creators but not bound to any specific cluster resource. At a later time, a cluster creator can create or expand a cluster by binding
available hosts to it.

## Motivation

The benefits of late binding are as follows:

- Adjusts the flow to suit the two different personas (infrastructure administrator and cloud creator).

- Allows for a more cloud-like use case where compute resources of different flavors can be consumed, similar to IBM Satellite.

- Allows agent-based installation to fit more naturally into a MAPI or CAPI model. While the details of such an integration are not in the scope of this enhancement, the idea is that hosts are booted, thus creating a collection of unassigned agents, and the machine creation involves choosing one such agent and beginning its installation.

- Reduces the number of Discovery ISOs, as there is no need to generate and download one for each cluster installation. Each ISO incurs capacity overhead for the service, and management overhead for the user which conflicts with our ease-of-use goal.

- Allows the user to see their inventory and then make decisions on how to build clusters based on hardware and connectivity. At a later stage, the service can make recommendations. This would be a layer of logic above late binding and is out of scope for this enhancement.

- Enables the user story where the Discovery ISO is loaded at the factory, and then discovered at a later time.

### Goals

Note: The language here refers to CRDs, but also holds true for the corresponding REST resources

- Make the InfraEnv and Agent resources independent from the ClusterDeployment. A single InfraEnv should have the potential to boot any number of hosts, whose Agents register with no relation to any particular ClusterDeployment. An Agent may be assigned to a ClusterDeployment at a later time.

- An Agent may be associated with a different ClusterDeployment at any time, unless it is part of an ongoing cluster installation.

### Non-Goals

Note: The language here refers to CRDs, but also holds true for the corresponding REST resources

- Once a host boots from disk, booting the Discovery ISO again to return it to the collection of free Agents is considered to be a manual step. There are several options that we will explore whereby this can be done automatically, but that discussion is out of scope for this enhancement.

- At a later stage, Agents may be automatically assigned to clusters based on their discovered inventory and/or network configurations. This is future work and not in the scope of this enhancement.

- For SaaS, there are discussions about changing the tenancy model to include organizations and not only users. Those decisions are out of scope for this document.

## Proposal

There are two main (non-breaking) changes to the CRDs:

1. ClusterRef in the InfraEnv Spec will no longer be mandatory.
1. ClusterRef in the Agent Spec will no longer be mandatory.

A Discovery ISO with no associated cluster means that hosts booted with that ISO will have agents that are not bound to any cluster. Thus, Agent CRs can be created with no cluster binding. It will still be possible to retain the previous behavior by providing a ClusterRef. The tenancy model will remain unchanged, with Agents being created in the namespace of the InfraEnv they booted from.

The Agent's ClusterRef may now be deleted or changed dynamically, unless the Agent is part of an ongoing cluster installation, in which case the installation must first be cancelled (not currently possible via the k8s API). If the host had started/completed its installation, a new Agent Condition would indicate that the user needs to boot the host into the Discovery ISO once
again (as mentioned in the non-goals, this can be done automatically but is not in the scope of this enhancement).

The REST API will require breaking changes, so we will create a v2 API. At the same time we will better align with the k8s API (change image to infra-env, but don't change host to agent because it will require too many changes).

1. Move images (infra-envs) to be a standalone resource, rather than part of the cluster resource (`/clusters/{cluster_id}/downloads/image` to `/infra-envs`). This removes the dependency of an image on a specific cluster, removes the current limitation of one image per cluster, and aligns with InfraEnv CRD.
1. An agent is now always referenced using a combination of the infra-env ID and host ID, and the REST API path will be `/infra-envs/{infra_env_id}/hosts/{host_id}` rather than `/clusters/{cluster_id}/hosts/{host_id}`. This means that a server that is booted from the ISO of infra-env1 and then from the ISO of infra-env2 will generate two Agents (the first's state should be `disconnected`). The Agent will have an optional cluster-id property (foreign key in the DB) that signifies which cluster the Agent is bound to, if any.
1. The InfraEnv resource will have an additional optional cluster-id property that will be used for non-late-binding flows. When an Agent registers itself, the assisted-service should check if the Agent's InfraEnv has the cluster-id property set, and if so, set that property on the Agent accordingly.

For the SaaS, the service will record ownership (user and org ID) for InfraEnvs, and Agents will inherit the same ownership.

Agent validations that do not depend on being part of a cluster will run for Agents that are both associated with clusters and those that are not. Examples of these include minimum hardware for any role, valid hostname, and registry access. Validations that do depend on being part of a cluster, such as connectivity checks, will run only for Agents associated with clusters.

Events are currently always scoped to a cluster. We will now need two types of events:

1. The events that we have today, which should be visible to the Cluster Creator.
1. The events for an Agent which is not associated with a cluster, for example a history of when it registered and which clusters it was associated with (should be visible to the infrastructure admin). These events should be scoped to an InfraEnv.

### User Stories

#### Story 1

As an Infrastructure Admin, I want to create a Discovery ISO and use it to boot hosts for use by Cluster Creators to install OpenShift on.

#### Story 2

As an Infrastructure Admin, I want to add Discovery ISOs to hosts as a boot option, and then ship the hosts to remote locations where they will be booted at a later time for use by Cluster Creators to install OpenShift on.

#### Story 3

As a Cluster Creator, I want to view a collection of available Agents and use them for OpenShift cluster creation or expansion.

#### Story 4

As a Cluster Creator, I want to reassign an Agent from one OpenShift cluster to another (not during installation).

### Implementation Details/Notes/Constraints

It would be beneficial to move the discovery image management to a separate service at some point, both to allow it to be scaled independently and to reduce the very large scope of the existing service. The late binding work should be done with this separation in mind, even if the actual separation is an orthogonal task.

### Risks and Mitigations

## Design Details

- Currently, a cluster ID is baked into the ISO, and the agent relays that to the service upon registration. With late binding, the InfraEnv ID should replace the cluster ID in this context. The controller will also use this information to create the Agent CRs in the same namespaces as their associated InfraEnvs.

- Internally, in the SQL DB, hosts currently have two primary keys - the host ID and the cluster ID. Now the primary keys will be host ID and InfraEnv ID.

- Currently the agent writes the image from the ISO to disk, which means all Agents that form a cluster need to be running with the same version. Instead, the agent should download the correct version for the cluster being formed if it doesn't match what's in the ISO.

- When moving a host from a source to dest cluster the service needs to make sure that the source cluster is not installing (can be before or after) and that the dest cluster is before installation. If the source cluster is before installation, the host will automatically go to disconnected state, which is the correct behavior. However, the service should update its state manually so it doesn't take 3+ minutes (e.g., by resetting the timestamp of when it last checked in). If the host doesn't exist in the dest cluster a new host will be created (register_host). If it does exist, we reset its state and properties but not its events.

- There should be one pull secret for the ISO to pull the necessary images (Infrastructure Admin's'), and another pull secret in the cluster definition for the installed cluster to use (Cluster Creator's).

- Token revocation currently relies on the cluster ID. It should now rely on the InfraEnv ID.

### Open Questions

### UI Impact

The UI will need to use the new REST APIs, but can keep the same flow and functionality. New screens will need to be created to add late binding functionality if we decide to expose it in the SaaS.

### Test Plan

New tests will be needed at all levels.

## Drawbacks

## Alternatives

Rather than keeping the existing DB schema, we could make image and host tables that are independent of clusters. This would be a much more difficult migration. It would also mean that we have no separation of host state between different cluster associations (e.g., we would need to clear host files and state when binding to a new cluster).
