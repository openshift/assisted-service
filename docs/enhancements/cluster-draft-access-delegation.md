---
title: cluster-draft-access-delegation
authors:
  - "@rivkyrizel"
creation-date: 2026-11-16
last-updated: 2026-11-16
---

# Delegating Edit Access for Cluster Drafts Prior to Installation

**Cluster draft**: a cluster registered in assisted-service but not yet
installed — AMS subscription in `Reserved`, no `external_cluster_id`. SaaS
(RHSSO) flow only.

## Summary

A Managed Service Provider under a single corporate Red Hat account needs two
stages: an administrator verifies the license is paid and creates the draft, then
a support engineer installs it. Today the engineer needs global `OCM Provisioner`
rights, which also lets them create unpaid clusters and defeats the financial
gate — so administrators run every installation themselves.

Subscription-based editing grants are already specified in
`docs/enhancements/tenancy-based-auth.md` and implemented in
`pkg/auth/rhsso_authz_handler.go`. What blocks the workflow is a separate,
resource-independent check requiring `create` on `BareMetalCluster` for *every*
API call. This proposal decouples the right to create a cluster from the right to
act on one you were granted, plus the corrections needed to make a grant safe and
revocable. Scoped to **delegation**, not ownership transfer — see
[Alternatives](#alternatives).

## Motivation

Compliance requires that no cluster be provisioned before the end customer has
paid, so *creation* must stay with administrators while *installation* is
delegated. Neither ownership transfer nor edit delegation works during the draft
phase today, which is the bottleneck this RFE is filed against.

### Goals

- A user with **no** global cluster-creation rights can fully install a single
  delegated draft: configure the cluster and a bound InfraEnv, get boot
  artifacts, discover hosts, install, retrieve credentials.
- The grant is scoped to that draft and its bound resources, and confers no
  ability to create or import other clusters.
- The administrator keeps ownership and can list and revoke grants within a
  stated maximum delay `T`.
- Revocation is not defeated by resources the engineer created.
- No user gains access they lack today, other than the delegated draft.

### Non-Goals

- Transferring ownership — `clusters.user_name` and the subscription's
  `AccountUsername` keep recording the creating administrator.
- Verifying payment or consuming entitlements in assisted-service. The gate is
  the administrator's act of creating the draft.
- Org-wide provisioning or grant-management authority for engineers.
- Kube-API / on-prem mode — the design rests on AMS, which has no analogue there.

## Proposal

### Root cause: why each attempted workaround failed

Two *independent* gates run on every RHSSO-authenticated request, in this order
(`pkg/auth/rhsso_authz_handler.go:301`):

1. **Global gate** — `allowedToUseAssistedInstaller` (`:380`, called at `:325`)
   issues `AccessReview(username, create, "", BareMetalClusterResource)`. Not
   parameterized by the requested resource; failure is `403` on everything.
2. **Per-resource gate** — `checkClusterBasedAccess` (`:200`) /
   `checkInfraEnvBasedAccess` (`:215`), reached only for `UserRole` (`:338`).
   Writes resolve through `hasSubscriptionAccess` (`:129`): the strict owner, or
   a holder of `AccessReview(update, <ams_subscription_id>, Subscription)` — the
   Cluster Editor grant. Failure returns `404` (`:360`), not `403`.

| Workaround | Gate 1 | Gate 2 | Observed |
|---|---|---|---|
| Cluster Editor only | **fails** | would pass | `403` everywhere, including `GET` of the delegated draft |
| Global Provisioner only | passes | **fails on write** | reads work; writes `404`, reading as "draft isn't there" |
| Both | passes | passes | works — but Provisioner permits creating clusters, defeating the gate |

The customer was never one setting away from success: both gates must pass, and
the only combination satisfying both is the one compliance forbids.

Three corollaries, each removing an item from the RFE's scope:

- **Check the configuration first.** Gate 2's subscription path is conditional on
  `isTenancyEnabled()` (`:39-40`) ← `ENABLE_ORG_TENANCY`, which defaults to
  `false` (`pkg/auth/authenticator.go:43`) and is set by no manifest here. If it
  is off in the affected environment, `hasSubscriptionAccess` never consults AMS
  and Cluster Editor could not work regardless — making the flag a large part of
  the fix. Confirm the production value before writing code.
- **The OCM CLI 404 is expected, not a defect.** A draft's subscription is created
  with `Reserve(true)` and no `external_cluster_id`
  (`pkg/ocm/accounts_mgmt.go:38-67`); that field is set only at install
  (`internal/bminventory/inventory.go:1467`), when status also moves to `Active`
  (`internal/cluster/cluster.go:1927`). CLI subcommands resolving through
  `clusters_mgmt` find nothing. Role bindings for a draft must target the
  **subscription**. Docs/CLI item, not an assisted-service change.
- **Pull-secret ownership transfer cannot apply.** That procedure re-associates an
  *installed* cluster; a draft's ownership lives in `clusters.user_name` and the
  subscription's `AccountUsername` (`pkg/ocm/accounts_mgmt.go:41`), neither
  touched by a pull secret update.

### User Stories

1. **Administrator gates and delegates.** Having confirmed an order, I create a
   draft, grant a named engineer in my organization edit access, and can later
   list and revoke every grant. I remain owner throughout.
2. **Engineer installs one approved draft.** With no cluster-creation rights, I
   find the delegated draft, create or edit its InfraEnv, download the ISO, boot
   and configure hosts, install, and retrieve credentials. Creating or importing
   any other cluster is denied.
3. **Administrator revokes mid-flight.** Within `T` the engineer can no longer
   modify the cluster, its InfraEnv — *including one they created* — or its
   hosts, nor mint new artifacts. Already-downloaded artifacts and issued tokens
   are beyond revocation; this limit is stated explicitly, not implied.

### Implementation

**1. Move the create check out of blanket middleware (core change).**
`ocmAuthorizer` stops applying `allowedToUseAssistedInstaller` to requests
targeting an existing resource, which are then authorized by the per-resource
gate. Creation routes (`v2RegisterCluster`, `v2ImportCluster`) keep the
`AccessReview(create, BareMetalCluster)` check at the route.
`RegisterInfraEnv` is allowed when `cluster_id` is set in the body and the caller
passes that cluster's update check; otherwise it requires the global create right,
keeping unbound late-binding InfraEnv creation provisioner-only. List routes stay
reachable or the console cannot surface the draft.

**Reads by callers without the create right must still require ownership or a
grant.** This part is load-bearing. `GET` resolves to `hasOwnerAccess` →
`isObjectOwnedByOrg` (`:119-123`) — org-wide read, today reachable only by callers
who already pass gate 1. Dropping gate 1 without this rule would let *any*
authenticated org member read *every* cluster in the organization and download its
kubeconfig and kubeadmin password, with no grant at all. Preserving today's
effective access instead — org-wide read for callers passing the create check,
ownership or a grant for everyone else — keeps the change strictly additive. The
same caveat applies to list results, filtered by the org-scoped `OwnedBy` (`:71`).

**2. Stop bundling delete into edit.** `checkClusterBasedAccess:205-209` maps both
`UpdateAction` and `DeleteAction` to `ocm.AMSActionUpdate`, so an editing grant is
also a deletion grant: the engineer can `DELETE /v2/clusters/{id}`, which also
deletes the `Reserved` subscription (`internal/bminventory/inventory.go:1229`).
That contradicts administrator authority and this repo's own spec —
`tenancy-based-auth.md` says delete requires *remove* access. `ocm.AMSActionDelete`
exists (`pkg/ocm/utils.go:18`) and is unused; route `DeleteAction` to it. Behavior
change for existing grant holders; needs AMS confirmation the role mapping exists.

**3. Make revocation survive engineer-created resources.**
`checkInfraEnvBasedAccess:246-250` grants write to the creator before consulting
the bound cluster, so an InfraEnv the engineer created stays writable after
revocation. For a **bound** InfraEnv the cluster check becomes authoritative and
the creator short-circuit is dropped; unbound InfraEnvs keep owner-only behavior.
Administrators are unaffected, passing the cluster check as owner. Changes
behavior for a user whose InfraEnv is later bound to a cluster they cannot edit —
call out in release notes.

**4. Decide what owns engineer-created records.** They are stamped with the
engineer's `user_name`, which drives `OwnedBy` filtering (`:71`) and makes the
database contradict the "administrator retains ownership" story. Either stamp the
delegating administrator, or keep the creator and add an explicit owner concept.
Main open design question.

**5. Bound the revocation delay `T`.** Not purely a policy choice: the OCM client
caches the authz payload per username for 10 minutes (`pkg/ocm/client.go:96`) and
`ocmAuthorizer:316-323` reads `IsAuthorized` from it. Neither the subscription
access review nor the ownership lookups are cached — `MGMT-9443 TODO: cache this`
at `:113` is still open — so per-draft revocation takes effect on the next
request. After change #1 the cached flag no longer gates delegated access and `T`
reduces to AMS propagation time. Any future caching of the per-resource decision
must ship with invalidation.

**6. Residual access after revocation — the real figures.** Discovery ISO URLs are
signed with a token defaulting to **4 hours** (`IMAGE_EXPIRATION_TIME`,
`internal/bminventory/inventory.go:109`), and `imageTokenAuthorizer` (`:272`)
validates only that the token's `sub` matches the requested InfraEnv ID — no user,
no grant — so no invalidation mechanism exists today. Already-downloaded ISOs and
retrieved credentials are permanently beyond revocation. These are system
properties, not open questions: the customer accepts them, or token invalidation
is scoped as separate work.

**Cross-component work.** AMS/OCM: confirm grants can be assigned, listed and
revoked on `Reserved` subscriptions, which role maps to `update` vs `delete`, and
propagation latency for `T`. Console: grant management on the draft, and the full
install journey for a user without Provisioner rights. OCM CLI/docs: document
binding roles against the subscription.

| Scenario | Applicability |
|---|---|
| REST / Kube-API | REST (SaaS, RHSSO) only; Kube-API unaffected |
| Late binding | Bound InfraEnv in scope; unbound creation stays provisioner-only |
| SNO / multinode / compact / 3+1 | Orthogonal — authorization is per cluster record |
| Day 1 / day 2 | Day 2 already resolves to the day-1 subscription (`:141-149`) |
| Day 2 on imported clusters | Import stays provisioner-only |
| Observability | New events distinguishing grant-mediated from owner actions |
| Networking / platforms / arch / OCP version / OLM / agent upgrade | Unaffected |

For a compliance-driven account, audit matters: `eventsapi.Handler` already
carries the acting username, but nothing marks an action as grant-mediated.

### Risks and Mitigations

- **Read scope is the principal risk.** Org-wide read already exists
  (`:119-123`), but today only callers passing the global create gate reach it.
  Removing that gate without the read rule above would expand read and credential
  access to every authenticated org member, grant or not. The proposal therefore
  makes ownership or a grant a precondition for reads by callers without the
  create right. If the customer prefers the simpler model — org-wide read for all
  members, delegation governing writes only — that is a deliberate expansion of
  access and must be accepted by them explicitly and in writing beforehand.
- **Relaxing the global gate widens the reachable API surface.** Mitigated by
  route-level create checks and the per-resource gate; tests must assert denial,
  not only permission.
- **Fail closed.** AMS unreachable returns `500` (`ocmAuthorizer:326`); any new
  path must preserve that. An authorization outage must never widen access.
- **Behavior changes** from #2 and #3 need release notes and an AMS role check.

## Design Details

### Open Questions

1. Is `ENABLE_ORG_TENANCY` enabled in the affected environment? How much of the
   reported behavior is configuration rather than a missing feature?
2. Which read model — grant-gated reads for non-provisioners, or org-wide reads
   for all members with customer sign-off?
3. Who owns resources the engineer creates (#4)?
4. Individual-user grants only, or are group grants required?
5. Which lifecycle actions does a grant cover: cancel, reset, reinstall, day-2
   workers, post-installation access?
6. Is the approval unit a draft, an installation attempt, or an entitlement? A
   draft that is reset and reinstalled is one draft but arguably two installs.
7. What `T` and residual-access window will the customer accept in writing?
8. Is CLI access actually needed, or is the console sufficient?

### UI Impact

Grant management on a draft (assign, list, revoke), and the full install journey
rendering for a non-provisioner, including surfacing the delegated draft in
cluster lists. Denials should be distinguishable from "not found" — today a failed
write check returns `404` (`:360`), presenting as a missing cluster.

### Test Plan

- **Unit.** Extend the table-driven harness in
  `pkg/auth/rhsso_authz_handler_test.go`: delegated user without create rights
  reaching cluster and InfraEnv endpoints; same user denied on register/import;
  **non-granted org member denied read and credential download**; delete rejected
  under an update-only grant; write denied on a self-created bound InfraEnv after
  revocation; `500` rather than allow on AMS errors.
- **Subsystem.** Delegated install against the mocked OCM client: grant → install
  → revoke mid-flight.
- **Manual QA (staging, real identities).** Agree `T`, credential lifetimes and an
  allowed-action matrix first, then verify assignment/listing/revocation while
  `Reserved`; a full install by a non-provisioner from a fresh session; denial of
  creation, import, grant management and unassigned drafts; revocation before,
  during and after discovery taking effect within `T`; and issued image tokens
  behaving exactly as documented.

## Drawbacks

This is a Red Hat-side authorization change driven by one customer's internal
financial process, which could instead be enforced organizationally — a separate
account per approval stage, each with its own provisioners. Splitting create from
act also makes an already intricate authorizer harder to reason about: three
interacting notions of access (strict ownership, org tenancy, subscription grant)
plus route-specific create checks, where the failure mode is silent
over-permission — as the read-scope risk demonstrates. The `404`-for-`403` masking
that confused this investigation also remains unaddressed.

## Alternatives

- **Ownership transfer (the RFE's primary request).** Rejected as primary: it
  requires mutating `AccountUsername` on the AMS subscription, a capability that
  does not exist, plus `clusters.user_name` locally — and it loses the
  administrator's access and audit trail, which the compliance story depends on.
  If AMS gains subscription reassignment, this becomes viable and simpler.
- **Native license-gating in assisted-service.** Rejected per non-goals:
  entitlement state belongs in AMS, and modeling it here couples the installer to
  commercial workflows.
- **Global Provisioner plus after-the-fact audit.** Rejected — the requirement is
  preventive, not detective.
- **Enable `ENABLE_ORG_TENANCY` and grant Cluster Editor, no code change.** A
  prerequisite to rule out, not an alternative: gate 1 still demands the create
  right. But it determines how much of this is genuinely new work.
- **Administrator creates the InfraEnv and hands over the ISO.** No code change,
  but the engineer still cannot drive installation or fix discovery problems.
