---
title: image-reconciliation-architecture
authors:
  - "@carbonin"
creation-date: 2026-10-09
last-updated: 2026-10-09
---

# Image Reconciliation Architecture

## Summary

Increase image-service availability during image configuration rollouts, primarily
for the SaaS deployment where large disconnected ISOs will be served.

Separate image configuration, artifact reconciliation, and image serving in
assisted-image-service. A long-running reconciler runs alongside the server
and downloads and prepares configured images in shared storage. The server serves
completed images while others are being prepared, both at startup and runtime.

All deployments move to this architecture. The first implementation provides an
environment variable source preserving existing catalog inputs and a mounted
ConfigMap/file-watch source for runtime configuration. SaaS deployment tooling
and the on-prem AgentServiceConfig controller provide the mounted catalog.

Remove the global image population barrier at image-service startup. A configured
image that is not ready yet receives the same pending-image response at startup and
runtime: HTTP 503 with a message explaining that the image is being prepared and
a retry hint. ISO download requests for images absent from the current
configuration continue to receive 404.

## Motivation

The primary use case is adding large disconnected ISOs to the SaaS image catalog
while keeping already prepared images available.

At [image-service startup][image-main], [readiness middleware][image-readiness]
blocks all image requests until every configured image has been downloaded and
required preparation has finished. This also blocks access to cached images
while newly configured images are prepared.

The [standalone image-service deployment template][image-deploy] embeds its
catalog in the pod template, as does the on-prem [AgentServiceConfig
controller][asc-controller]. Catalog changes therefore trigger a rollout and
repeat the startup barrier. Mounting the catalog and separating reconciliation
from serving lets prepared images remain available during these changes.
Removing the startup barrier extends the same behavior to server restarts.

### Goals

Increase service availability during new image configuration rollouts.

### Non-Goals

- Change assisted-service's startup catalog or deployment rollout behavior.
- Add an image type field or OVE enablement to AgentServiceConfig.
- Change image URLs, customization, host binding, or installation flows.

## Proposal

Each image-service pod runs a server container and a long-running reconciler
container using the same container image, with a separate binary for the
reconciler. The reconciler selects a configuration source through an environment
variable and brings shared storage into agreement with current desired
configuration.

The server uses the same source and filename rules to determine which images
should exist. It checks availability under file locks and serves images whose
required files are available. It does not reconcile the filesystem or wait for
the whole catalog to be populated.

The first implementation includes:

- **Environment variables:** Preserve existing inputs, precedence, and defaults.
  Configuration is fixed for the process lifetime; changing it requires a
  rollout. These deployments also adopt both containers.
- **Mounted ConfigMap/file watch:** Read desired configuration from a file and
  reevaluate it when that file changes, including mounted ConfigMap changes.
  Both processes consult this source without restarting.

The source describes current desired state. Source read/parse errors must be
reported rather than treated as a configuration that removes every image.

`/health` reports readiness once the server can consult configuration and safely
handle availability checks. Catalog convergence is not a readiness condition,
including when every image is pending. The reconciler's probe behavior must
follow the same principle so pod readiness does not recreate the startup barrier.

### User Stories

#### Story 1: Keep SaaS images available during catalog rollouts

As a SaaS service operator adding large disconnected ISOs, I want completed
images accessible while newly configured images are downloaded.

#### Story 2: Remove an image using existing configuration semantics

As a service operator, I want removing an image from configuration to make it
inaccessible to new requests.

#### Story 3: Deploy and operate the architecture

As a service operator, I want to deploy this architecture in SaaS independently
of AgentServiceConfig, with serving health distinguishable from reconciliation
progress. As an on-prem operator maintainer, I want the controller to provide
the same availability benefit for full and minimal ISOs.

### Implementation Details/Notes/Constraints

#### Configuration and file identity

Preserve [existing configuration parsing][image-main], image lookup, artifact
naming, and [cache reuse][image-store]. Both server and image reconciler derive
expected files from the same configuration and existing preparation rules.

The server checks configuration membership and file availability under reader
locks. Images absent from configuration remain inaccessible even if an old file
remains on disk. Files shared by remaining configured entries must be retained.

#### Files and locking

Use read/write file locks across processes. Writers protect creation and removal;
readers acquire a lock before checking/opening files and hold it through
response completion. Requests for images being prepared return the pending
response without waiting for the writer. Apply this coordination to all image
and boot artifact readers. Removal waits for active readers to finish.

[Downloads already use temporary files and atomic rename][image-store].
Some generated artifacts currently write directly to final paths, so interrupted
writes can already leave partial files today. Extend atomic publication to all
artifacts: prepare and validate temporary files, then rename them to their final
names under the writer lock.

Kernel-managed locks such as [flock][file-locks] release when all owning
descriptors close, including on process exit. Choose the locking API and verify
filesystem and mount behavior during implementation. Runtime cleanup must
preserve desired prepared files and respect active readers and writers.

#### Request behavior

For ISO download requests:

| Image state | Response |
| --- | --- |
| Configured and required files available for reading | Normal ISO download response. |
| Configured and required files missing or being prepared | 503 with a message explaining that the image is being prepared and a retry hint. |
| Absent from current configuration, including a removed image | 404, even if an old file remains on disk. |
| Configured but preparation has failed terminally | An error distinct from the pending-image 503; exact code remains open. |

The response message identifies the pending-image state. Pending responses use
`Retry-After` and `Cache-Control: no-store`. HTTP 503 with `Retry-After` supplies
the temporary unavailability signal described by [RFC 9110][http-503]; it does
not guarantee completion by the next request.

[Boot artifact][image-boot] and [initrd][image-initrd] requests read from the full
ISO and use the same pending response while their configured ISO or required
artifacts await preparation or are being prepared. Existing errors for
configuration absence remain unchanged.

#### Deployment changes

Update container builds, deployment tooling, and CI to run both containers with
shared storage. Preserve existing storage, permissions, trust, and download
settings. Each reconciler manages its pod's store.

SaaS deployment configuration must mount the desired catalog into both
containers. Catalog-only changes must not change the pod template or restart
the image service. This deployment path is independent of AgentServiceConfig;
the [standalone deployment template][image-deploy] also needs adoption.

#### AgentServiceConfig controller changes

The [AgentServiceConfig controller][asc-controller] must deploy both containers,
reconcile its effective catalog into a mounted ConfigMap, and configure both
processes to use the file source without catalog-triggered restarts.
Use the existing AgentServiceConfig API for on-prem full and minimal images;
adding disconnected ISO support to that API is outside this effort.

#### assisted-service behavior

assisted-service retains its [startup catalog][service-main] and existing
rollout behavior. [Customized image requests][image-iso] still depend on its
availability for ignition and other content during a rollout.

### Risks and Mitigations

- **Reader/writer races:** Use locks and temporary-file/rename publication;
  test concurrent serving, removal, and interrupted preparation.
- **Readiness and clients:** Readiness no longer means every image is available.
  Separate serving health from convergence and verify consumer retries.
- **Storage and resources:** Include temporary transfers and workspaces in
  capacity planning and measure concurrent serving/reconciliation.
- **Deployment adoption:** Verify both SaaS and on-prem deployment paths,
  including upgrades and shared permissions.

## Design Details

### Open Questions

1. **Failures and shutdown:** What retry/backoff and cancellation policies apply?
   How are source read/parse errors surfaced? When is preparation failure terminal,
   and how does the server learn about it?
2. **HTTP and consumers:** Settle the retry interval, response message wording,
   and terminal preparation error code. Which existing consumers need retry changes?
3. **Observability:** Which logs, metrics, health checks, events, and controller
   status reporting belong in this effort?

### UI Impact

No UI changes are specified. Presentation of pending or failed preparation
depends on the eventual status and error reporting contract.

### Test Plan

Use unit and integration tests for both configuration sources, request handling,
and separate server/reconciler processes using shared storage and real locks.
Cover startup and runtime availability, removal during active reads, interrupted
preparation, terminal failures, cache reuse, and cleanup. Exercise existing ISO
and PXE consumers through REST and Kubernetes APIs across supported image
configurations, including pending-response retries.

The main availability test adds a large disconnected ISO through SaaS mounted
configuration while its download remains in progress. Prepared images must
remain accessible, the new image must return the pending response, and it must
become accessible when ready. Repeat with the image pending at server startup.
Measure interruption of prepared-image requests during the rollout.

Verify catalog-only changes leave the image-service pod template unchanged in
SaaS and controller-managed on-prem deployments. Cover on-prem full/minimal
images, the shared Hypershift deployment path, and adoption by existing
environment-based deployment and CI tooling. Test upgrades and supported
filesystem locking behavior.

## Drawbacks

Every deployment gains a process and cross-process coordination. Consumers must
handle pending images, and a ready server may initially have none available.
Filesystem behavior and concurrent cleanup need testing.

## Alternatives

- **Wait for every image at startup:** Retain the population barrier, then allow
  partial availability at runtime. This delays access to completed images.
  Runtime additions already require a pending response; using it at startup
  provides consistent availability behavior.
- **Reconcile inside the server:** A source and reconciliation loop could live
  in the existing process. This avoids a second container but keeps serving and
  mutation together. Separate processes give serving and reconciliation
  independent lifecycles. Additionally a separate loop would need its own monitoring
  and health checks.
- **Optimize startup population only:** Improve transfer/preparation performance.
  [Downloads already run concurrently][image-store]. This approach retains the
  catalog-triggered restart and barrier.

[image-main]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/main.go
[image-store]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/pkg/imagestore/imagestore.go
[image-readiness]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/internal/handlers/readiness.go
[image-iso]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/internal/handlers/iso.go
[image-boot]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/internal/handlers/boot_artifacts.go
[image-initrd]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/internal/handlers/initrd.go
[asc-controller]: ../../internal/controller/controllers/agentserviceconfig_controller.go
[service-main]: ../../cmd/main.go
[image-deploy]: https://github.com/openshift/assisted-image-service/blob/efaaa9e8fb7baabbbcb62b9b8af625a366203dcc/deploy/template.yaml
[http-503]: https://www.rfc-editor.org/rfc/rfc9110.html#section-15.6.4
[file-locks]: https://man7.org/linux/man-pages/man2/flock.2.html
