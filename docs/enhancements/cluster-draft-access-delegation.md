---
title: cluster-draft-access-delegation
authors:
  - "@rivkyrizel"
creation-date: 2026-11-16
last-updated: 2026-11-16
---

# Delegating Edit Access for Cluster Drafts Prior to Installation

Throughout this document, **cluster draft** means a cluster record that has been
registered in assisted-service but not yet installed: its AMS subscription is in
`Reserved` status and it has no `external_cluster_id`. This is the SaaS
(Hybrid Cloud Console) flow only.

## Summary

A Managed Service Provider operating under a single corporate Red Hat account
needs a two-stage install workflow: an administrator verifies that a license has
been ordered and paid for and creates the cluster draft, and a support engineer
then completes discovery and installation of that specific draft. Today the
engineer must either be given global `OCM Provisioner` rights — which lets them
create unpaid clusters and so defeats the financial gate — or the administrator
must perform the entire installation themselves.

The capability the customer is asking for is largely specified and partially
implemented already: `docs/enhancements/tenancy-based-auth.md` defines
subscription-based editing grants, and `pkg/auth/rhsso_authz_handler.go`
implements them. What blocks the workflow is an unconditional, resource-independent
authorization check that runs ahead of the per-resource one and requires
`create` permission on `BareMetalCluster` for *every* API call. This enhancement
proposes decoupling the right to create a cluster from the right to act on a
cluster you have been granted access to, plus four smaller corrections needed to
make a delegated grant safe and revocable.

This proposal is deliberately scoped to **delegation**, not ownership transfer.
See [Alternatives](#alternatives) for why.

## Motivation

The customer sells software bundled with an OpenShift runtime and support. All
clusters live under one corporate Red Hat account. Financial compliance requires
that no cluster be provisioned before the end customer has ordered and paid for
the license, so cluster *creation* must stay restricted to administrators while
cluster *installation* is delegated to support engineers.

Because neither ownership transfer nor effective edit delegation works during the
draft phase, administrators are currently forced to run every installation
themselves. This is the operational bottleneck the RFE is filed against.

### Goals

- A user with **no** global cluster-creation rights can complete discovery and
  installation of a single draft that has been explicitly delegated to them:
  configure the cluster and a bound InfraEnv, obtain boot artifacts, discover and
  configure hosts, install, monitor, and retrieve the resulting credentials.
- The delegated grant is scoped to that draft and the resources bound to it, and
  does not confer the ability to create or import additional clusters.
- The administrator keeps ownership and can enumerate and revoke the grant, with
  a stated, bounded revocation delay.
- Revocation is not defeated by resources the engineer created along the way.

### Non-Goals

- Transferring ownership of a draft or a cluster. The AMS subscription's
  `AccountUsername` and the assisted-service `user_name` column both continue to
  record the creating administrator.
- Having assisted-service verify payment, approve licenses, or consume purchase
  entitlements. The gate is the administrator's act of creating the draft; the
  service only enforces who may act on it afterwards.
- Granting engineers organization-wide provisioning or grant-management authority.
- Kube-API / on-prem mode. The whole design rests on AMS and has no analogue
  there; `LocalAuthzHandler` and `NoneAuthzHandler` are unaffected.
- Changing the existing organization-wide **read** model (see
  [Risks](#risks-and-mitigations)).

## Proposal

### Root cause: why each attempted workaround failed

This matters more than the proposal itself, because three of the four reported
failures are explained by existing code and one of them may be a configuration
problem rather than a missing feature.

Two *independent* authorization gates run on every RHSSO-authenticated request,
in this order (`pkg/auth/rhsso_authz_handler.go:301`):

1. **Global gate** — `allowedToUseAssistedInstaller` (`:380`), reached from
   `ocmAuthorizer` at `:325`, issues
   `AccessReview(username, create, "", BareMetalClusterResource)`. It is not
   parameterized by the requested resource. If it fails, the request is rejected
   with `403` regardless of what was asked for.
2. **Per-resource gate** — `checkClusterBasedAccess` (`:200`) /
   `checkInfraEnvBasedAccess` (`:215`). Write actions resolve through
   `hasSubscriptionAccess` (`:129`), which allows the strict owner or any user
   holding `AccessReview(update, <ams_subscription_id>, Subscription)` — i.e. the
   Cluster Editor grant. Failure here is reported as `404 Object Not Found`
   (`:360`), not `403`, unless the user has read-but-not-write access.

That ordering explains the reported behavior:

| Workaround | Gate 1 | Gate 2 | Observed |
|---|---|---|---|
| Cluster Editor only (no Provisioner) | **fails** | would pass | `403` on every call, including `GET` of the delegated draft |
| Global Provisioner only (no Cluster Editor) | passes | **fails on write** | reads work; writes return `404`, which reads as "the draft isn't there" |
| Provisioner **and** Cluster Editor | passes | passes | works — but Provisioner also permits creating clusters, defeating the license gate |

So the customer was never one setting away from success: the two gates must both
be satisfied, and the only combination that satisfies both today is precisely the
one their compliance model forbids.

**A configuration check must come first.** The entire subscription-grant path in
gate 2 is conditional on `isTenancyEnabled()` (`:39-40`), backed by
`ENABLE_ORG_TENANCY`, which defaults to `false` (`pkg/auth/authenticator.go:43`)
and is not set by any manifest in this repository. If it is disabled in the
affected SaaS environment, `checkInfraEnvBasedAccess` falls back to strict
ownership (`:220-221`) and `hasSubscriptionAccess` never consults AMS at all — in
which case Cluster Editor could not work no matter what, and enabling the flag is
a large part of the fix. Confirm the production value before any code is written.

**The OCM CLI 404 is expected, not a defect.** During the draft phase the
subscription is created with `Reserve(true)` and no `external_cluster_id`
(`pkg/ocm/accounts_mgmt.go:38-67`); `external_cluster_id` is only populated at
install time (`internal/bminventory/inventory.go:1467`) and the status only moves
to `Active` then (`internal/cluster/cluster.go:1927`). `ocm` CLI subcommands that
resolve a cluster through `clusters_mgmt` therefore find nothing. Role bindings
for a draft must be made against the **subscription** in `accounts_mgmt`, not
against a cluster. This is a documentation and CLI-ergonomics problem; it does not
need an assisted-service change.

**Pull-secret-based ownership transfer cannot apply to a draft.** That procedure
re-associates an *installed* cluster. A draft's ownership lives in two places —
`clusters.user_name` in the assisted-service database and `AccountUsername` on the
AMS subscription (`pkg/ocm/accounts_mgmt.go:41`) — and neither is affected by a
pull secret update.

### User Stories

#### Story 1: Administrator gates and delegates

As an administrator who has confirmed an end customer's order, I create a cluster
draft in the console, then grant a named support engineer in my organization edit
access to that draft. I can later list every grant on the draft and revoke any of
them. I remain the owner and retain full access throughout.

#### Story 2: Support engineer installs one approved draft

As a support engineer with no cluster-creation rights, I sign in to the console,
find the draft delegated to me, create or edit its InfraEnv, download the
discovery ISO, boot and configure hosts, start the installation, watch it
progress, and download the kubeconfig and kubeadmin password when it finishes.
Attempting to create or import any other cluster is denied.

#### Story 3: Administrator revokes mid-flight

As an administrator, I revoke an engineer's grant while discovery is in progress.
Within a defined maximum delay `T`, the engineer can no longer modify the cluster,
its InfraEnv — *including one they created themselves* — or its hosts, and can no
longer mint new discovery artifacts. Artifacts already downloaded and tokens
already issued are out of reach of revocation; this limit is stated to the
customer explicitly rather than implied.

### Implementation Details/Notes/Constraints

#### 1. Move the creation check out of the global middleware (the core change)

`ocmAuthorizer` must stop applying `allowedToUseAssistedInstaller` to requests
that target an existing resource. Proposed rule:

- Requests carrying a `cluster_id` or `infra_env_id` path parameter are
  authorized **solely** by the per-resource gate. A user with a valid
  subscription grant and no global create right is allowed through.
- Creation routes — `v2RegisterCluster`, `v2ImportCluster` — keep the
  `AccessReview(create, BareMetalCluster)` check, enforced at the route rather
  than in blanket middleware.
- `RegisterInfraEnv` is the subtle one: the engineer legitimately needs to create
  an InfraEnv for the delegated cluster. Proposed rule: allow it when
  `cluster_id` is set in the request body and the caller passes the cluster's
  update check; otherwise (unbound InfraEnv) require the global create right.
  This keeps late-binding InfraEnv creation a provisioner-only operation.
- List routes (`v2ListClusters`, `ListInfraEnvs`) have no resource parameter.
  They must remain reachable or the console cannot surface the delegated draft;
  results continue to be filtered by `OwnedBy` (`:71`).

Removing the blanket check also removes the only consumer of `payload.IsAuthorized`
and of the per-username authorization cache described in §5 below.

#### 2. Stop bundling delete into edit

`checkClusterBasedAccess:205-209` maps both `UpdateAction` and `DeleteAction` to
`ocm.AMSActionUpdate`, so an editing grant is also a deletion grant — the engineer
can `DELETE /v2/clusters/{id}`, which additionally deletes the `Reserved` AMS
subscription (`internal/bminventory/inventory.go:1229`). This contradicts the goal
that the administrator retains authority, and it contradicts this repository's own
specification: the table in `tenancy-based-auth.md` says delete requires *remove*
access. `ocm.AMSActionDelete` already exists (`pkg/ocm/utils.go:18`) and is unused.

Route `DeleteAction` to `ocm.AMSActionDelete`. Note this is a behavior change for
existing editing-grant holders and needs an AMS-side confirmation that the role
mapping exists.

#### 3. Make revocation survive engineer-created resources

`checkInfraEnvBasedAccess:246-250` grants write unconditionally to the record's
creator before consulting the bound cluster. An InfraEnv created by the engineer
therefore stays writable after the grant is revoked — they can keep regenerating
ISOs and managing its hosts indefinitely.

For an InfraEnv **bound** to a cluster, the bound cluster's access check becomes
authoritative and the creator short-circuit is dropped; unbound InfraEnvs keep
today's owner-only behavior. The administrator is unaffected, since they pass the
cluster check as its owner. This does change behavior for a user whose InfraEnv is
later bound to a cluster they cannot edit; that is the intended semantics but
should be called out in release notes.

#### 4. Decide what owns engineer-created records

Resources the engineer creates are stamped with their `user_name`. That affects
`OwnedBy` list filtering (`:71`) and means the database does not reflect the
"administrator retains ownership" story. Options: stamp the delegating
administrator as owner, or keep the creator and add an explicit owner concept.
This needs a decision before implementation — it is the main open design question
(see [Open Questions](#open-questions)).

#### 5. Define and bound the revocation delay `T`

`T` is not purely a policy choice; code already sets a floor. The OCM client caches
the authorization payload per username for 10 minutes
(`pkg/ocm/client.go:96`, `cache.New(10*time.Minute, 30*time.Minute)`), and
`ocmAuthorizer:316-323` reads `IsAuthorized` from it. Neither the subscription
access review in `hasSubscriptionAccess` nor the ownership lookups are cached —
the `MGMT-9443 TODO: cache this` at `:113` is still open — so per-draft
revocation already takes effect on the next request.

With change #1, the cached global flag no longer gates delegated access, and `T`
reduces to AMS grant-propagation time. If any caching of the per-resource decision
is added later, it must come with invalidation. State the resulting `T` to the
customer as a number.

#### 6. Residual access after revocation — state the real figures

Revocation cannot recall what has already been handed out:

- Discovery ISO URLs are signed with a token whose default lifetime is **4 hours**
  (`IMAGE_EXPIRATION_TIME`, `internal/bminventory/inventory.go:109`).
- `imageTokenAuthorizer` (`:272`) validates only that the token's `sub` claim
  matches the requested InfraEnv ID. It checks no user and consults no grant, so
  there is no mechanism to invalidate an issued image token today.
- Already-downloaded ISOs, and kubeconfig/kubeadmin credentials already retrieved,
  are permanently out of scope of revocation.

These are properties of the system, not open questions. The customer must accept
them, or a token-invalidation mechanism must be scoped as separate work.

#### Cross-component work

- **AMS/OCM:** confirm that editing grants can be assigned, listed, and revoked on
  `Reserved` subscriptions; confirm which role maps to `update` versus `delete`;
  confirm grant propagation latency for `T`.
- **Console:** a grant-management surface on the draft, and the full discovery and
  installation journey rendered for a user without Provisioner rights.
- **OCM CLI / docs:** document binding roles against the subscription for drafts.
  Optionally make the CLI resolve drafts by subscription rather than 404.

#### Scenario coverage

| Scenario | Applicability |
|---|---|
| REST API / Kube-API | REST (SaaS, RHSSO auth) only; Kube-API unaffected |
| Late binding / bind-on-discovery | Bound InfraEnv in scope; unbound InfraEnv creation stays provisioner-only (§1) |
| SNO / multinode / compact / 3+1 | Orthogonal — authorization is per cluster record |
| Day 1 / day 2 | Day 2 already resolves to the day-1 subscription (`:141-149`); adding workers to a delegated cluster follows the same grant |
| Day 2 on imported clusters | Import stays provisioner-only |
| Observability | New events for grant-mediated actions (see below) |
| Network config / platforms / CPU arch / OCP version / OLM operators | Unaffected |
| Agent upgrade | Unaffected |

Auditability deserves emphasis: for a permissions feature in a compliance-driven
account, the event stream should record that an action was performed by a
delegated user rather than the owner. The existing `eventsapi.Handler` already
carries the acting username; the gap is that nothing distinguishes owner actions
from grant-mediated ones.

### Risks and Mitigations

- **Reads are already organization-wide, and this proposal does not change that.**
  `GET` resolves to `hasOwnerAccess` → `isObjectOwnedByOrg` (`:119-123`), so any
  member of the organization can already read any cluster in it and download its
  credentials — before any grant exists and after it is revoked. For a single-
  corporate-account MSP this may be acceptable or may be a surprise; it must be
  stated plainly to the customer, because it means delegation controls *writes*,
  not *visibility*. Narrowing the read model is possible but is a change to
  shipped tenancy behavior affecting all users, and is deliberately out of scope
  here.
- **Relaxing the global gate widens the reachable API surface for non-provisioners.**
  Mitigated by enforcing create at the creation routes and by the per-resource
  gate on everything else; the test plan must assert denial, not just permission.
- **Failing closed.** If AMS is unreachable, the current code returns `500` from
  `ocmAuthorizer:326`. Any new path must preserve that: an authorization outage
  must never widen access.
- **Behavior changes for existing users** from §2 and §3 need release notes and an
  AMS-side role check before rollout.

## Design Details

### Open Questions

1. Is `ENABLE_ORG_TENANCY` enabled in the affected SaaS environment? If not, how
   much of the reported behavior is configuration rather than a missing feature?
2. Who owns resources the engineer creates (§4), and what should the console show
   as their owner?
3. Does the customer need group-based grants, or are individual-user grants
   sufficient for the initial scope?
4. Which lifecycle actions does a grant cover: cancel, reset, reinstall, adding
   workers day 2, and access after installation completes?
5. Is the approval unit a draft, an installation attempt, or a purchase
   entitlement? A draft that is reset and reinstalled is one draft but arguably
   two installs.
6. What `T` and what residual-access window (§6) will the customer accept in
   writing?
7. Does the customer actually need CLI access, or is the console sufficient? This
   determines whether the OCM CLI work is in scope at all.

### UI Impact

The console needs a grant-management affordance on a draft (assign, list, revoke),
and every step of the discovery and installation journey must render for a user
without Provisioner rights — including surfacing the delegated draft in cluster
lists. Denials should be distinguishable from "not found"; today a failed write
check returns `404` (`:360`), which presents to the user as a missing cluster.

### Test Plan

- **Unit.** `pkg/auth/rhsso_authz_handler_test.go` already has a table-driven
  harness over these paths with a mocked `OCMAuthorization`. Extend it for: a
  delegated user with no create right reaching cluster and InfraEnv endpoints; the
  same user denied on register/import; delete rejected under an update-only grant
  (§2); write denied on a self-created bound InfraEnv after revocation (§3); and
  `500`, not allow, on AMS errors.
- **Subsystem.** A delegated-install scenario against the mocked OCM client,
  covering grant → install → revoke mid-flight.
- **Manual QA (staging, real identities).** Agree `T`, credential lifetimes and an
  allowed-action matrix first, then verify: assignment/listing/revocation while
  `Reserved`; a full install by a non-provisioner from a fresh session; denial of
  creation, import, grant management and unassigned drafts; revocation before,
  during and after discovery taking effect within `T` including through
  engineer-created resources; and that previously issued image tokens behave
  exactly as documented in §6.

## Drawbacks

The strongest argument against this proposal is that it is a Red Hat-side
authorization change driven by one customer's internal financial process, when
that process could be enforced organizationally — for example by a separate Red
Hat account per approval stage, with its own provisioner set. Splitting the
create right from the act right also makes an already intricate authorizer harder
to reason about: there would be three interacting notions of access (strict
ownership, organization tenancy, subscription grant) plus route-specific create
checks, and the failure mode of getting it wrong is silent over-permission.

It is also worth noting that the "404 instead of 403" masking that confused this
investigation will keep confusing people until it is addressed, and this proposal
does not address it.

## Alternatives

- **Ownership transfer (the RFE's primary request).** Reassign the draft to the
  engineer outright. Rejected as the primary approach because it requires mutating
  `AccountUsername` on the AMS subscription — a cross-service capability that does
  not exist — plus `clusters.user_name` locally, and because it loses the
  administrator's access and auditability, which the compliance story depends on.
  Delegation achieves the business goal with a smaller blast radius. If AMS gains
  subscription reassignment, this becomes viable and simpler.
- **Native license-gating in assisted-service.** Have the service itself enforce
  approval before provisioning. Rejected per the non-goals: entitlement and
  payment state belong in AMS, and modeling them here would couple the installer
  to commercial workflows.
- **Give engineers global Provisioner and audit after the fact.** Rejected by the
  customer's compliance requirement, which is preventive, not detective.
- **Enable `ENABLE_ORG_TENANCY` and grant Cluster Editor, with no code change.**
  Not an alternative so much as a prerequisite to rule out first (see the root
  cause section). It cannot be sufficient on its own, because gate 1 still demands
  the create right — but it determines how much of this proposal is actually new
  work.
- **Administrator creates the InfraEnv and hands over the ISO.** No code change,
  but the engineer still cannot drive installation or fix discovery problems, so
  the bottleneck remains.
