---
title: label-clusterimagesets-with-version-and-arch
authors:
  - "@mhrivnak"
creation-date: 2022-02-23
last-updated: 2022-02-23
---

# Label ClusterImageSets with version and arch


## Summary

Label each ClusterImageSet resource with two labels: the architecture of the
release image, and the OpenShift version. This will enable assisted-service to
understand the releases and find a particular version and architecture when
needed.

## Motivation

When installing clusters with an architecture other than x86_64,
assisted-service requires the user to specify OpenShift release images for both
the x86_64 architecture and the target architecture, at the same version. This
is because regardless of the target architecture, assisted-service will run the
openshift-install binary on its own cluster as part of the process for creating
installation manifests and ignition. For example, if a user wants to install
OpenShift 4.9.21 on arm, they would utilize a ClusterImageSet resource that
references `quay.io/openshift-release-dev/ocp-release:4.9.21-aarch64`.
Assisted-service would then need to understand the version and architecture for
that image and then locate an x86_64 image that is also version 4.9.21.

### Goals

* Enable assisted-service to understand the architecture and OpenShift version
of each ClusterImageSet.

### Non-Goals

* Add other inspected information about a release in the form of annotations or labels.

## Proposal

A new controller will be added to assisted-service. It will watch
ClusterImageSet as its primary resource. For each resource, it will ensure that
two labels exist:

`agent-install.openshift.io/openshift-version`: The output of `oc adm release info -o=jsonpath='{.metadata.version}'`
`agent-install.openshift.io/architecture`: The output of `oc adm release info -o=jsonpath='{.config.architecture}'`

### User Stories

#### Story 1

As a user, I can identify a ClusterImageSet of a particular version and
architecture.

### Implementation Details/Notes/Constraints [optional]

Anywhere an assisted-service reconciler needs to find a ClusterImageSet with
a specific version and architecture, it should look for the appropriate labels.
If not found, it should set a condition or otherwise communicate on the
appropriate resource (likely AgentClusterInstall) that the required release is
not found. It should requeue its resource for some retry period. In the future,
some more advanced coupling could be implemented such that when a
ClusterImageSet gets labeled, that triggers reconciliation of
AgentClusterInstalls that are waiting for a release, but that is out of scope
for the first implementation.

### Risks and Mitigations

None.

### Open Questions

None.

### UI Impact

None.

### Test Plan

Automated tests should ensure that a ClusterImageSet gets the correct labels.

e2e tests should ensure that the labels are used successfully to install a
cluster on arm.

## Drawbacks

None.

## Alternatives

These labels could be useful to other users. In that case, it might make sense
to implement this in hive, so that it automatically labels ClusterImageSets.
We will share this proposal with the hive team for feedback.
