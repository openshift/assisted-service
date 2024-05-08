---
title: fips-with-multiple-rhel-versions
authors:
  - "@carbonin"
creation-date: 2024-05-07
last-updated: 2024-05-08
---

# Support FIPS for installers built for different RHEL releases

## Summary

In order for an OpenShift cluster to be considered FIPS compliant the installer
must be run on a system with FIPS mode enabled and with FIPS compliant openssl
libraries installed. This means using a dynamically linked `openshift-install`
binary against the openssl libraries present on our container image. Today this
is not a problem because all `openshift-install` binaries in use have been
expecting to link to RHEL 8 based openssl libraries, but now OpenShift 4.16 will
ship an installer that requires RHEL 9 libraries.

This will require assisted-service to maintain a way to run the
`openshift-install` binary in a compatible environment for multiple openssl
versions. Specifically FIPS-enabled installs for pre-4.16 releases will need to
be run on an el8 image and 4.16 and later releases will need to be run on an
el9 image (regardless of FIPS).

## Motivation

FIPS compliance is important for our customers and assisted-service should be
able to install FIPS compliant clusters.

### Goals

- Allow for a single installation of assisted-service to install FIPS-compliant
  clusters using installer binaries built against RHEL 8 or RHEL 9

- Allow for FIPS compliant clusters to be installed from the SaaS offering or
  the on-prem offering

### Non-Goals

- Changing cluster install interfaces to accommodate new FIPS requirements
  should be avoided

- Dynamically determining a given release's RHEL version. Assisted service will
  track the minimum version for using el9 and if a version can't be determined
  for some reason (FCOS may not have the same versioning scheme) el9 will be
  the default.

## Proposal

Two additional containers will run alongside assisted-service in the same pod.
These "installer-runner" containers will expose a HTTP API local to the pod
using a unix socket. assisted-service can then choose which API to contact to
run an installer binary for a specified release to generate the manifests
required for a particular install. These manifests will then be uploaded to
whatever storage is in use for this deployment (local for on-prem, or s3 for
SaaS) and assisted-service will take over as usual from there.

The installer cache directory will be shared (as it's currently on the PV), but
the installers used by the two runners will never overlap.

The installer runners will be built with the required packages to run the
installer in FIPS compliance mode.

### User Stories

#### Story 1

As a Cluster Creator, I want to install FIPS compliant clusters for any supported OpenShift version

### Implementation Details/Notes/Constraints

#### New Images

Two new container images will need to be built, and published for every release
footprint we support. These images will be created based on existing
assisted-service code, but could be split into their own independent projects
later.

### Risks and Mitigations

Shipping a new image is a non-trivial process. This may take more time than we
have to set up. We could likely get away with using the existing
assisted-service image with a different entrypoint for one of the runner images,
but that still requires us to publish a new one for the architecture
assisted-service will not be using.

## Design Details [optional]

- A new `installer-runner` service will be created, written in go.
- The installer-runner will be compiled twice: once in a RHEL 8 builder image,
  and once in a RHEL 9 builder image, with each resulting binary being placed
  into a RHEL base image of corresponding version.


The new runner containers will expose a HTTP server using a unix socket.
assisted-service will POST to one of these servers when it needs manifests generated.
The runner container will respond with any error that occurred while generating
the manifests or with success in which case assisted-service will assume the
manifests were created and uploaded successfully.

### Open Questions

1. What does the API look like for the runner containers? What data should be
   passed in an API call and what should be configured in the container
   environment?
2. What specific packages are required for these new images?

### UI Impact

No impact

### Test Plan

FIPS and regular installs should be tested for all supported OpenShift versions.
Since this should be mostly transparent to the user, regression testing in
addition to testing 4.16 with and without FIPS should be sufficient.

## Drawbacks

This is a complicated change in architecture something simpler might be more
desirable.

Creating two additional containers in a pod makes the assisted service more
expensive to scale.

Creating, maintaining, and releasing additional images is a non-trivial amount
of additional work.

## Alternatives

Hive is investigating using the container image from the release payload to run
the installer. This seems possible, but doesn't make much sense if we want to
run a persistent service rather than a job per cluster. Running a job per
cluster wouldn't scale particularly well and would also be an even larger
architectural change. This would also be impossible for the podman deployment to
adopt.

Clusters that have installers that already match the assisted service container's
architecture could be handled by the assisted-service container as we do today.
This would require one less image and container per pod, but having the same
process for every cluster install would be easier to understand and maintain.

[Go's RPC](https://pkg.go.dev/net/rpc@go1.22.3) could be used instead of a direct
HTTP server (RPC can be hosted over HTTP, but that's not what is being addressed
here). RPC would make this a simpler change as the code for generating the
manifests is already contained in a single package, but RPC would be a strange
choice if we were to move the handling into a truly separate service in the
future.
