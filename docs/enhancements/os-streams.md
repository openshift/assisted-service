---
title: os-streams
authors:
  - "@giladravid16"
creation-date: 2026-06-14
last-updated: 2026-06-14
---

# Allow multiple OS Images per Release Image

## Summary

Currently, each Release Image matches a single OS Image by their OCP version and
CPU architecture pair. However, with the release of RHCOS 10, users will be able
to choose the os stream they want (rhel-9 or rhel-10 for now).

## Motivation

Users want to be able to choose which os stream to use, and we need to provide
a way for them to install clusters and hosts with their preferred os stream.

### Goals

Provide a way for users to choose an os stream when installing clusters and hosts.

### Non-Goals

Installing a cluster with hosts that use different os streams.

## Proposal

In the API, we will:
- Add a new field called `os_stream` in clusters
- Add a new API endpoint that returns the list of os streams for a given cluster

In the kube-api we will:
- Add a new field called `osStream` in AgentClusterInstalls

We will use the OCP version, CPU architecture and os stream (or their default values)
to determine which OS Image we need to provide.

### User Stories

#### Story 1

As a cluster administrator I want to be able to install my cluster with RHCOS 10
instead of installing with RHCOS 9 and then upgrading afterward.

### Implementation Details/Notes/Constraints

The main changes we'll make are with our current OS_IMAGES list and how we use
it with our Release Images.

Currently, both assisted-service and assisted-image-service assume Release Images
and OS Images have a one-to-one relationship, since for each Release Image there's
only one OS Image with the same OCP version and CPU architecture pair, and vice versa.
This doesn't only interfere with our goal in this proposal, it also results in a lot
of OS Images referencing the same RHCOS images. So we'll remove those assumptions
and change the relationship of Release Images and OS Images to a many-to-many one.
We'll do this by taking a Release Image and using `oc` to get both the default
os stream and the full list of available os steams, with the RHCOS version of each one.
We will then use the RHCOS version and CPU architecture pair to find the OS Image.

Let's use `quay.io/openshift-release-dev/ocp-release:4.22.0-rc.5-x86_64` as an example.
Assuming we have a valid pull request, if we go to an empty directory and run:
```
oc image extract --path /manifests/:. $(oc adm release info --image-for installer quay.io/openshift-release-dev/ocp-release:4.22.0-rc.5-x86_64)
```
We will get a single file called `coreos-bootimages.yaml` that has everything we need.
It's a configmap with 2 keys whose values are jsons - `stream` and `streams`.
The `stream` key is the default os stream, and the `streams` key is the list
of os streams. If the Release Image doesn't support multiple os streams then
the `streams` key doesn't exist, but the `stream` key does. We can get the
default RHCOS version and the RCHOS version for the `rhel-10` os stream with:
```
DEFAULT_RHCOS_VERSION=$(yq -r .data.stream coreos-bootimages.yaml | jq .architectures.x86_64.artifacts.metal.release)
RHEL10_RHCOS_VERSION=$(yq -r .data.streams coreos-bootimages.yaml | jq '."rhel-10".architectures.x86_64.artifacts.metal.release')
```

The other changes are more straightforward.

The `os_stream` field on clusters will match the `osStream` field for
AgentClusterInstalls and will be used for:
- Choosing the OS Image together with the OCP version and CPU architecture
- It will be passed on to the install-config for the installation

The `osImageVersion` field in the kube-api and the corresponding `openshift_version`
on infraenvs are used to determine the OS Image. They are currently set to the OCP
version and can only be used either in late-binding and when adding hosts to an
installed cluster (can be useful if the cluster was upgraded). We can repurpose
these fields to be set to the RHCOS version, but still allow setting it to the
OCP version if the value doesn't match any RHCOS version (in which case it will
use the default os stream).

### Risks and Mitigations

Increasing the amount of OS Images we keep in our image-service can lead
to us needing more storage. The changes we'll make in the OS_IMAGES list
will help minimize the storage usage.

### Open Questions

Our automation that bumps OCP and RHCOS versions will need includes OS Images
from multiple os streams. Currently, to find the RHCOS images, we look in
https://mirror.openshift.com/ for each OCP version and CPU architecture pair.
But there are going to be multiple RHCOS images for a single pair, so we need
to know to find each of them. It's not currently known how we'll do that - for example
maybe each os stream will have a new subdirectory or be part of the live iso's name.

### UI Impact

If the OCP version is 5.0+, choosing an os stream will be GA. So for those versions,
the UI should call the API mentioned above that will return the os streams for a
given cluster, and allow the user to choose one of them.

### Test Plan

We will add an e2e test that uses a specific os stream.
We'll need to also update assisted-test-infra for this.

## Drawbacks

This solution will take longer to implement in comparison to the alternative below.

## Alternatives

Instead of allowing multiple Release Images to use the same OS Image (if they use the
same RHCOS version), we could add a new field for the os stream on the OS Image struct.
However, this will negatively affect our storage usage in the long run.
