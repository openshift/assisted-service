---
title: dual-stack-vips
authors:
  - "@mkowalski"
creation-date: 2022-08-07
last-updated: 2022-10-31
tracking-link:
  - https://issues.redhat.com/browse/MGMT-9915
see-also:
  - https://github.com/openshift/enhancements/blob/master/enhancements/network/on-prem-dual-stack-vips.md
---

# Dual-stack VIPs

## Summary

Originally Assisted Installer supported only single-stack IPv4 or IPv6 deployments. When dual-stack support was added, we carried over the VIP configuration that only allowed for a single VIP to be specified.

The initial rationale was that in dual-stack clusters every required communication can happen over IPv4 and only the workload running in the cluster may need IPv6 connectivity.

Given this is no longer the case and that OCP for the baremetal platform supports dual-stack VIPs, this design is proposing the addition of a Cluster property allowing configuration of the aforementioned feature.

## Motivation

The current implementation of dual-stack support assumes that everything in the cluster should be reachable via IPv4 and that IPv6 is an addition for the workload running in the cluster. This assumption makes it impossible to interact with such a cluster using IPv6-only client, i.e. makes it impossible for IPv6-only Hub to manage a dual-stack Spoke.

### Goals

- Allow configuring both IPv4 and IPv6 VIP for dual-stack clusters.
- Allow configuring only IPv4 VIPs for dual-stack clusters (it is supported already today and will not be removed with implementation of this enhancement).

### Non-goals

- Allow configuring more than single IPv4 and single IPv6 VIP. This is still not supported by the OCP. MetalLB is a better solution to creating arbitrary loadbalancers.
- Allow configuring IPv6 as the primary networking stack. This is still not supported by the OCP. For dual-stack deployments the IPv4 stack is always to be the primary.
- Allow DHCPv6 allocation of IPv6 VIPs. This is because DHCPv6 is not the model way of deploying IPv6 networks as well as that we intentionally decided not to offer IPv4 DHCP allocation for dual-stack clusters.
- Allow VIP(s) to be placed outside of subnet(s) defined as machine network(s).
- Change behaviour of VIPs in a single-stack scenario.

## Proposal

### User Stories

- As an operator of the IPv6-only Hub cluster I want to deploy dual-stack Spoke cluster.

- As an user of a dual stack OCP cluster I want to run IPv6-only applications that need access to the API and/or Ingress VIPs.

### API Extensions

New fields will have to be added to the Cluster object to persist the specified VIPs.

### Implementation Details

Our change can be simply described as "making a singular field plural". For this, we will follow the [Kubernetes API change guide](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md#making-a-singular-field-plural).

#### Phase 1

`cluster-create-params`, `v2-cluster-update-params` and `cluster` will get new fields

- `api_vips` of type `array`
- `ingress_vips` of type `array`

Respective AgentClusterInstall CR will also get those new fields.

The database schema will be updated by creating new tables `api_vips` and `ingress_vips` with the following columns

- `cluster_id`
- `ip`

Creation of the new tables in the database happens transparently thanks to the golang libraries in use. Copying current data to the new tables will be done using DB migration (as defined by the GORM library).

Given that all the newly introduced fields are optional, there is no need to release a new version of the API.

#### Phase 2

A set of functions for cluster creation will provide a compatiblity between already existing fields (`api_vip` and `ingress_vip`) and new ones (`api_vips` and `ingress_vips`). The behaviour will be as follows

- If only `api_vip` is provided, the `api_vips` will be automatically populated by a simple conversion from `string` to `[]string`. This guarantees no change in behaviour for users not aware of introduction of the new field.

- If both `api_vip` and `api_vips` are provided, the value of `api_vips[0]` must match the value of `api_vip`.

- If only `api_vips` are provided, the error will be raised. This is the case when plural field is specified and the singular is not. The rationale is that in an update it's impossible to tell the difference between an old client clearing the singular field via patch and a new client setting the plural field. The update semantics must not differ from create, therefore behaviour as described.

For cluster update the following logic will apply

- If `api_vip` is cleared and `api_vips` are not provided, we will clear both fields. This is a scenario when an old client (not aware of plural field) is used.

- If `api_vips` is cleared and `api_vip` is not provided, the change will not apply. This is a scenario when an old client is used and it can't send fields it doesn't know about.

- If `api_vip` is changed (but not cleared), `api_vips` will be forcefully set to the value of `api_vip` as a one-element list. This is a scenario when an old client is used to change the field it knows about.

All the points described above will behave in an equivalent way for `ingress_vip` and `ingress_vips`. The logic above covers all CRUD operations that are reasonable for a specific combination in order to provide compatibility between various versions of clients.

#### Phase 3

After providing a required period of backwards-compatibility the `api_vip` and `ingress_vip` fields will be deprecated and removed. The value in the database will be nulled. Removal of the column in the database will follow.

The new version of API will be released as we are removing an existing field.

### UI Impact

UI changes will be required in order to

- display details of dual-stack clusters with dual-stack VIPs correctly
- allow configuring dual-stack VIPs for dual-stack clusters

For displaying the changes, the UI will need to display the value of `api_vips` and `ingress_vips` instead of the current `api_vip` and `ingress_vip`.

For setting the values, the UI will need to include `api_vips` and `ingress_vips` in the cluster payload. The previous fields i.e. `api_vip` and `ingress_vip` must not be provided.

Given that the implementation will provide a backwards compatibility, the rollout of the UI changes does not have to be synchronized with the rollout of the backend.

As UI is a web client, it's going to transition to the new API faster than other clients. Users will only need to refresh the browser in order to get the latest client version.

### Tooling impact

The process outlined here ultimately leads to the deprecation of some API fields and introduction of the new ones. This has impact on all the tooling that contacts the Assisted Service API. With the relation to the mentioned before phases, the following applies

- Phase 1

  - no impact for tooling; no changes needed

- Phase 2

  - no changes needed for tools that only use single-stack VIPs
  - changes needed for tools that want to offer dual-stack VIPs

- Phase 3
  - changes needed for all the tools

### Risks and Mitigations

The similar manouever of introducing `[]string` in place of `string` has already happened in the past when implementing the initial dual-stack support. An example of the field that got transformed is `machine_network_cidr` that became `machine_networks`.

There is already an experience and established path, therefore the risk is considered minimal.

Given that the core of the feature is implemented in the baremetal networking component of the OCP, the implementation in Assisted Installer is only responsible for providing the correct syntax to the install-config.

Before moving from phase 2 to phase 3 we need to make sure that developers of the supported tools around Assisted Service are aware of this change. This includes but is not limited to:

- aicli
- Crucible
- siteconfig generator

### Test Plan

The new fields will be covered by unit tests. The backwards compatiblity described above will be covered by unit tests.

The whole of the installation will be covered by respective E2E tests using both test-infra as well as dev-scripts. This will provide a sufficient coverage for both REST API and Kube API flows.
