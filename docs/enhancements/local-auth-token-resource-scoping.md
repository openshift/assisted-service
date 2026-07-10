---
title: assisted-service-local-auth-token-resource-scoping
authors:
  - "@bluesort"
creation-date: 2026-07-08
last-updated: 2026-07-10
---

# Assisted-Service Local Auth Token Resource Scoping

## Summary

Add resource-scoped authorization to the assisted-service's `AUTH_TYPE=local` mode by verifying that the resource ID in a LocalJWT token's claim matches the resource being accessed by the request, and replacing the permissive `NoneHandler` authorizer with a resource-aware implementation.

## Motivation

In operator deployments (MCE/ACM), the assisted-service uses `AUTH_TYPE=local`. The JWT tokens generated for InfraEnv ISO download URLs carry an `infra_env_id` or `cluster_id` claim, but the authenticator discards this after verifying the resource exists — any valid token grants access to all resources via the REST API. This includes the ability to download cluster credentials (kubeconfig, kubeadmin-password) for clusters the token holder has no relationship to.

The tokens are visible in the InfraEnv status (as part of the download URL), so any user who can read an InfraEnv in their namespace can extract one. From there, the events endpoint returns data across all namespaces (including cluster IDs), and the credentials endpoint serves any cluster's kubeconfig without checking ownership.

Even within a single namespace, the current behavior is overly permissive: a user who can only read InfraEnv CRs (and therefore extract an InfraEnv token) gains access to cluster credentials, logs, and events for clusters they have no Kubernetes RBAC permission to access.

In the current implementation:

- `AUTH_TYPE=local` is hardcoded in all operator deployments.
- `gencrypto.LocalJWT()` creates tokens with only `infra_env_id` or `cluster_id` claims.
- `LocalAuthenticator.AuthAgentAuth` and `AuthURLAuth` return `ocm.AdminPayload()` (a generic admin identity) for any valid token.
- `NewAuthzHandler` falls through to `NoneHandler` for `TypeLocal`, which applies no ownership filtering: `OwnedBy` returns unfiltered DB queries, `HasAccessTo` always returns true, and `IsAdmin` always returns true.

### Goals

- Enforce that the resource ID in a token's claim matches the resource being accessed by the request endpoint. An `infra_env_id` token can only access InfraEnv endpoints for that specific InfraEnv. A `cluster_id` token can only access cluster endpoints for that specific cluster.
- Scope the events endpoint so that a token can only retrieve events for the resource in its claim. When querying host events with an `infra_env_id` token, verify the host belongs to the token's InfraEnv.

### Non-Goals

- **Per-user access control.** This enhancement addresses resource-level scoping of `urlAuth` tokens, not per-user identity. The `AUTH_TYPE=local` model does not carry user identity.
- **Token expiration.** Adding `exp` claims to tokens is a separate concern from resource scoping and is out of scope for this enhancement.
- **Token rotation or revocation infrastructure.** Tokens embedded in CRD status fields cannot be rotated without updating the CRD.
- **New token claims.** No new claims are added to LocalJWT tokens. The existing `infra_env_id`/`cluster_id` claims are sufficient to scope access.
- **Changes to `AUTH_TYPE=rhsso` or `AUTH_TYPE=agent-installer-local`.** These authentication modes are not affected.

## Proposal

This enhancement enforces that `urlAuth` tokens can only access the specific resource identified by their claim:

1. Update `LocalAuthenticator` to propagate the token's resource claim type and ID in the request context (via the auth payload), instead of discarding it after existence verification.
2. Replace `NoneHandler` for `TypeLocal` with a `LocalAuthzHandler` that defaults to denying access for scoped tokens. Endpoints must explicitly verify the token's resource ID against the requested resource to permit access.
3. Enforce resource-scoped filtering on the events endpoint: the token's claim must match the queried resource, and host event queries with an `infra_env_id` token must verify the host belongs to that InfraEnv.

The default-deny behavior ensures that any future `urlAuth` endpoint that lacks explicit resource verification is denied by default, rather than silently granting access.

### Workflow Description

#### Current flow (insecure)

1. A user creates an InfraEnv CR in namespace `tenant-a`.
2. The InfraEnv controller generates a LocalJWT token with claim `{infra_env_id: "<id>"}` and embeds it in the ISO download URL written to `infraEnv.Status.ISODownloadURL`.
3. Any user who can read the InfraEnv status extracts the token.
4. The token authenticates against `LocalAuthenticator.AuthURLAuth`, which verifies the InfraEnv exists and returns `AdminPayload()`.
5. `NoneHandler.OwnedBy` applies no filtering — the token holder can query events for all resources, discover cluster IDs, and download credentials for any cluster.

#### Proposed flow (resource-scoped)

1. A user creates an InfraEnv CR in namespace `tenant-a`.
2. The InfraEnv controller generates a LocalJWT token with claim `{infra_env_id: "<id>"}` and embeds it in the ISO download URL.
3. The token authenticates against `LocalAuthenticator.AuthURLAuth`, which verifies the InfraEnv exists and stores the claim type (`infra_env_id`) and value in the context payload.
4. On each `urlAuth` endpoint, the `LocalAuthzHandler` verifies the token's resource ID matches the resource being accessed:
   - `GET /v2/infra-envs/{infra_env_id}` — token's `infra_env_id` must match the path parameter.
   - `GET /v2/clusters/{cluster_id}/downloads/files` — token's `cluster_id` must match the path parameter.
   - `GET /v2/events?infra_env_id=X` — token's `infra_env_id` must match the query parameter.
   - `GET /v2/events?host_id=Z` with an `infra_env_id` token — the host must belong to the token's InfraEnv.
5. Attempting to access a different resource returns 404 Not Found.

### API Extensions

No new endpoints. No API signature changes. No new token claims. The `v2ListEvents` endpoint gains resource-scoped filtering when accessed through `urlAuth`.

### Affected Endpoints

Each `urlAuth` endpoint already has the resource ID available in its path or query parameters. Verification is a direct comparison:

| Endpoint | Token claim | Request field | Verification |
|---|---|---|---|
| `GET /v2/infra-envs/{infra_env_id}` | `infra_env_id` | path param | exact match |
| `GET /v2/infra-envs/{id}/downloads/files` | `infra_env_id` | path param | exact match |
| `GET /v2/infra-envs/{id}/downloads/minimal-initrd` | `infra_env_id` | path param | exact match |
| `GET /v2/clusters/{id}/downloads/files` | `cluster_id` | path param | exact match |
| `GET /v2/clusters/{id}/logs` | `cluster_id` | path param | exact match |
| `GET /v2/events` | either | query params | see below |

**Events endpoint scoping:**

The events endpoint accepts `cluster_id`, `infra_env_id`, and `host_id` as query parameters. When accessed via `urlAuth`:

- If the token has `infra_env_id` and the query filters by `infra_env_id`: exact match required.
- If the token has `cluster_id` and the query filters by `cluster_id`: exact match required.
- If the token has `infra_env_id` and the query filters by `host_id`: verify the host belongs to the token's InfraEnv.
- If the token has `cluster_id` and the query filters by `host_id`: verify the host belongs to the token's cluster.
- Any other combination (e.g., an `infra_env_id` token querying by `cluster_id`): rejected.

No changes to controllers are required. They already generate the correct token type per URL (`InfraEnvKey` for InfraEnv endpoints, `ClusterKey` for cluster endpoints).

### Feature Flags

`LOCAL_AUTH_ENFORCE_RESOURCE_SCOPE` (default `true`) enables resource-ID verification — set to `false` for emergency rollback.

### Security Considerations

This enhancement is fundamentally a security fix. The current state allows any valid token to access any resource, including credential theft.

**Threat model addressed:**
- A user in namespace A extracts a LocalJWT token from an InfraEnv download URL.
- The token currently grants admin-level access to all resources via the REST API.
- After this enhancement, the token can only access the specific InfraEnv identified by its claim — not cluster credentials, not other InfraEnvs, not resources in other namespaces.

**Compared to namespace-scoping:** Resource-scoping provides strictly tighter access control. A namespace-scoped approach would allow an InfraEnv token to access cluster credentials in the same namespace, which violates Kubernetes RBAC intent — a user with only `get infraenvs` permission should not be able to download cluster kubeconfigs.

**Residual risks:**
- Tokens are embedded in CRD status fields, which are readable by any user with `get` access to the InfraEnv or ClusterDeployment resource. This is inherent to the `AUTH_TYPE=local` design.
- Tokens have no expiration. A stolen token remains valid for its specific resource indefinitely. Token expiration is a separate concern that can be addressed independently.
- The ECDSA signing key (`EC_PRIVATE_KEY_PEM`) is stored in a Kubernetes Secret with no rotation mechanism. Compromise allows forging tokens for any resource. Rotating the key invalidates all existing tokens until controllers reconcile.

**Risks:**
- Breaking existing deployments during upgrade is mitigated by the `LOCAL_AUTH_ENFORCE_RESOURCE_SCOPE` feature flag for rollback. Existing tokens already carry the correct resource ID claims, so resource scoping applies immediately with no token regeneration needed.

### Failure Handling

- **Resource ID mismatch:** 404 Not Found.
- **Claim type mismatch** (e.g., `infra_env_id` token on a cluster endpoint): 404 Not Found.

### Observability and Monitoring

Structured log events and Prometheus counters will be added for: resource ID mismatch (cross-resource access blocked) and successful scoped access.

### Drawbacks

- **Host-to-InfraEnv verification on the events endpoint** adds one DB lookup when an `infra_env_id` token queries events by `host_id`. This is bounded to the events endpoint and uses an indexed foreign key.

## Alternatives (Not Implemented)

**Alternative 1: Namespace-scoped authorization**

Instead of verifying the token's resource ID against the request, derive the resource's namespace from the DB and scope all access to that namespace. A `LocalAuthzHandler` would filter queries with `WHERE kube_key_namespace = ?`.

*Why not chosen:* Namespace scoping is broader than necessary. An InfraEnv token scoped to namespace `tenant-a` would grant access to all clusters, hosts, and credentials in `tenant-a` — even if the token holder only has Kubernetes RBAC permission to read InfraEnvs. This misaligns with Kubernetes RBAC intent and leaves a privilege escalation path within the namespace. Resource-ID verification uses information already present in the token claims without introducing namespace concepts to the authorization layer.

**Alternative 2: Remove `AUTH_TYPE=local` entirely and require RHSSO**

Operator deployments could be configured to use `AUTH_TYPE=rhsso` with an identity provider.

*Why not chosen:* This would break all existing operator deployments and require significant infrastructure changes (IdP setup, OIDC configuration). It is not feasible as a near-term fix.

**Alternative 3: Reuse `ImageTokenKey` from RHSSO presigning**

The RHSSO path uses a per-InfraEnv HS256 symmetric key (`ImageTokenKey`, stored in `internal/common/db.go:234`) to sign image download URLs with expiration (`JWTForSymmetricKey` in `gencrypto/token.go:60`). This mechanism could be extended to local auth.

*Why not chosen:* `ImageTokenKey` is InfraEnv-only — there is no equivalent field on `Cluster` or `Host`. Extending it to cluster-level endpoints (logs, events) requires schema additions. The `LocalAuthenticator` currently rejects `AuthImageAuth` entirely, so a new verification path would be needed. The per-resource key isolation is attractive (compromise of one key doesn't affect others), but the scope of changes is comparable to the resource-scoping approach while covering fewer endpoints. Worth revisiting if per-resource key isolation becomes a requirement.

**Alternative 4: Embed `namespace` claim directly in token**

Instead of deriving the namespace from the resource ID at authentication time, embed a `namespace` claim directly in the token at signing time.

*Why not chosen:* Requires changes to token generation across all controllers (each must pass the namespace). Introduces a namespace recycling risk: if a namespace is deleted and re-created with the same name, tokens signed with the old namespace claim remain valid for the new namespace. Also suffers from the same over-broad scoping as Alternative 1.

## Open Questions

1. **Are there external consumers of the events endpoint that rely on cross-resource queries via `urlAuth`?** If so, they need a migration path.
2. **What happens when the signing key rotates?** The ECDSA key is auto-generated once and stored in the `assisted-servicelocal-auth` Secret. Rotating it invalidates all existing tokens. Is there a grace period mechanism needed (accept tokens signed by the previous key for a configurable window)?

## Test Plan

**Unit tests:** `LocalAuthenticator` propagates resource type and ID in context. `LocalAuthzHandler` rejects mismatched resource IDs. Events endpoint host-to-InfraEnv ownership verified.

**Integration tests:** Create an InfraEnv and a cluster. Verify the InfraEnv token can access only that InfraEnv's endpoints, not the cluster's credentials or logs. Verify the cluster token can access only that cluster's endpoints. Verify events endpoint returns only events for the token's resource. Verify host event queries with an InfraEnv token succeed only when the host belongs to that InfraEnv.

**E2E tests:** Deploy operator, create InfraEnvs and clusters, verify download URLs work and cross-resource access is blocked.

**Edge cases:** Token claim type mismatch (InfraEnv token on cluster endpoint), host belonging to a different InfraEnv, feature flag toggle, concurrent operations, endpoints without explicit verification logic are denied by default.

## Graduation Criteria

**Dev Preview:** All subtasks merged. Cross-resource requests return 404. Unit, integration, and edge case tests pass.

**Tech Preview:** Feature flag operational. Upgrade/downgrade e2e test passes.

**GA:** At least one MCE/ACM minor release cycle with Tech Preview enabled and no P1/P2 regressions.

## Upgrade / Downgrade Strategy

**Upgrade (N → N+1):**
- No user action required. Existing tokens already carry the correct resource ID claims, so resource scoping applies immediately with no token regeneration needed.

**Downgrade (N+1 → N):**
- N applies no resource scoping. Tokens work but without restrictions. No data migration needed.

**Version coexistence:**
- The service and controllers are deployed as part of the same operator bundle, so version skew is limited to rolling update windows. The assisted-image-service requires no changes.
