---
title: fips-with-multiple-rhel-versions
authors:
  - "@carbonin"
creation-date: 2024-05-07
last-updated: 2024-05-20
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

### API

The new services would effectively wrap the existing `InstallConfigGenerator`
interface.

API call input:
- common.Cluster json
- install config
- release image

API call output:
- Appropriate HTTP response
- Error message if the call was not successful

### Installer Cache

The installer cache directory will be shared (as it's currently on the PV), but
the installers used by the two runners will never overlap.

### Packages

The installer runners will be built with the required packages to run the
installer in FIPS compliance mode.

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

- This is a complicated change in architecture something simpler might be more
  desirable.

- Creating two additional containers in a pod makes the assisted service more
  expensive to scale.

- Creating, maintaining, and releasing additional images is a non-trivial amount
  of additional work.

## Alternatives

### Use Jobs

Hive is investigating using the container image from the release payload to run
the installer as a Job.

- This wouldn't work for the podman deployment which isn't directly productized
  or supported, but is still a way we encourage people to try out the service.
  This could be overcome by retaining a way to run the installer on the
  service container, but then both methods need to be tested and maintained.

- This wouldn't work for Agent Based Installer as ABI runs the services using
  podman. This could also be overcome by retaining a way to run the installer
  local to the service as the image version run by ABI will always match the
  target cluster, but again both methods of running the installer would need to
  be maintained indefinitely.

- It's unclear how many jobs we would end up running concurrently. It would be
  difficult to find out from the SaaS how many installer processes are being run
  concurrently (maybe we should create a metric for this), but the telco scale
  team regularly runs several hundred concurrently maxing out at over three
  thousand in a few hours. Unless we're cleaning up the jobs rather aggressively
  I don't think it would be good to create this many.

- Multiple jobs would need to be run on a single assets directory. This seems
  prohibitively complex compared to the proposed solution. During a single
  install the following installer commands are used:
  - `openshift-baremetal-install create manifests`
  - `openshift-baremetal-install create single-node-ignition-config` or
    `openshift-baremetal-install create ignition-configs` (depending on HA mode)

### Run the matching installer on the assisted-service container

Clusters that have installers that already match the assisted service container's
architecture could be handled by the assisted-service container as we do today.
This would require one less image and container per pod, but having the same
process for every cluster install would be easier to understand and maintain.

### Use RPC over HTTP

[Go's RPC](https://pkg.go.dev/net/rpc@go1.22.3) could be used instead of a direct
HTTP server (RPC can be hosted over HTTP, but that's not what is being addressed
here). RPC would make this a simpler change as the code for generating the
manifests is already contained in a single package, but RPC would be a strange
choice if we were to move the handling into a truly separate service in the
future.

### Install multiple libraries on the same image

It may be possible to install both versions of the shared libraries required by
the installers (libcrypto and libssl?) for FIPS compliance on a single image.
This would require much less change and should be significantly quicker to
implement, but it's not clear if these would be possible or supportable.
This could be achieved by any of the following methods:

1. Create a separate userspace for el8 libraries and chroot when those libraries
   are required.
  - This seems a bit complicated and it will likely make our image quite a bit
    larger than it already is (~1.3G).
2. Install both versions of the required packages simultaneously.
  - Not sure if this is possible given that the packages share a name and are
    only different in version.
3. Use multi-layer container builds to copy the libraries from an el8 image to a
   directory on the el9 image and use `LD_PRELOAD` or manipulate `LD_LIBRARY_PATH`
   to point the el8 installer binaries to the correct libraries.

The approach using chroot worked, but FIPS SMEs said that the container base
image *must* match the installer for the resulting cluster to be considered
FIPS-compliant so none of these multi-library options are valid.

### Publish multiple assisted-service images

It's likely that a user installing in FIPS mode will only be interested in
installing a single OCP version at a time. This means that a given version of
assisted will need to still support both el8 and el9 FIPS installs, but a single
deployment of assisted would not.

To handle this the assisted-service image would be built twice; once based on
el8 and again based on el9. Both images would be released and the operator would
choose which to deploy based on configuration (likely an annotation since a more
robust solution would be preferred in the future).

For example, in the case that a user knew they wanted to deploy OCP 4.14 from a
FIPS-enabled hub cluster they would need to indicate to the operator that the
el8-based assisted-installer should be deployed. Assisted service could also
check that the OCP version, current base OS, and FIPS-mode were all aligned
before attempting to run the installer.

To avoid issues when installing in a non-FIPS environment the assisted-service
could also move to default to the statically linked installer binary for OCP
4.16 and above, but this doesn't change anything for earlier releases.

This would be something that could be implemented more quickly with less risk
while also leaving open the possibility of a more complex solution to the general
problem in a future release.
