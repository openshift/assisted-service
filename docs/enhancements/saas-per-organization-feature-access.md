---
title: saas-per-organization-feature-access
authors:
  - "@nmagnezi"
creation-date: 2022-05-02
last-updated: 2022-05-02
---

# SaaS Per-Organization Feature Access

The current OCM-based authorization scheme used by assisted service as a part of the SaaS operation does not support exposing features to certain users.
We wish to allow the following:

- An ability to expose selected group(s) of users to assisted-service functionality (in OCM, the term is `capability`).
- The user grouping should be based on the organization ID.

## Motivation

Some features might be of interest to partners or early adopters.
Other features might not be ready for public consumption: tech-preview or an internal-only functionality.
Exposing such features to a pre-determined group of users can help us to gather feedback and improve while keeping the other users with a production-level user experience.

### Goals

- Add a generic method to assisted service authorization middleware to assert against an organization's capability. The method should query OCM based on the payload and asserted capability name.
- Implement a functionality-specific assertion for `ARM`.

## Proposal

Initially, the authorization middleware should have a method to check if the user belongs to an organization with a particular capability.
Adding such a method would be the first step, both to assert organization capabilities, such as ARM support, and any other capability of interest in the future.

The API endpoints that need ARM support feature enablement:

### V2ListSupportedOpenshiftVersions API

- [ReleaseImage](../../models/release_image.go) has a `CPUArchitecture` attribute.
- Expected to filter out ARM version if that capability is not included for the user organization.

### V2RegisterCluster API

- [ClusterCreateParams](../../models/cluster_create_params.go) has a `CPUArchitecture` `attribute.
- Expected to return `HTTP 400` (BadRequest) if `ARM` got selected and that capability is not included for the user organization.

### RegisterInfraEnv API

- [InfraEnvCreateParams](../../models/infra_env_create_params.go) has a `CPUArchitecture` attribute.
- Expected to return `HTTP 400` (BadRequest) if `ARM` got selected and that capability is not included for the user organization.

Each of the APIs mentioned above should make use of the new authorization middleware method to make a decision based on the organization's capability existence/absence.

An AMS API call to check for capability will only happen when relevant. In this case, only if the selected CPU architecture (both for cluster and infraEnv) is ARM.

### Risks and Mitigations

- The lack of organization capabilities caching might have a SaaS performance impact. See [Open Questions](#Open_Questions).
- This enhancement is relevant for assisted SaaS, but should not change anything for on-prem. For that, we will include a feature flag to disable this when needed.

### Open Questions

Based on internal discussions, we understand that organization capabilities are not cached in AMS. we should:

1. Understand the performance impact, if any.
2. Find an alternative, if needed. We can, for example, cache it locally as done in `isReadOnlyAdmin()` [here](../../pkg/auth/rhsso_authenticator.go).

**Note:** The API endpoints involved in ARM CPU architecture are not expected to be called for many times by each user, but this might not be the case for other future capabilities.

### UI/UX Impact

Unlike the UI, the backend will adapt its response to a change in organization capabilities on each API call.
For ARM specifically, this is less relevant as the user cannot change the CPU architecture post cluster and infra_env creation, yet it might play a role in other future features.
The UX for those cases will be discussed when it becomes relevant.

The UI should determine which CPU architectures are supported and accessible to the user by querying The AMS API for the user organization capabilities.
It sends a request to AMS subscriptions (inherited from the organization) via `api.openshift.com/api/accounts_mgmt/v1/subscriptions/`.
This request gets sent on each UI refresh.

There's a field named `items`, and in that field, a list of `capabilities` in the response payload.
this request gets sent on each UI refresh.

## Alternatives

1. Use OCM roles instead, which will group users based on their email domains.
