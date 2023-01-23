---
title: dynamic-fetching-of-openshift-versions
authors:
  - "@osherdp"
creation-date: 2023-01-12
last-updated: 2023-01-12
---

# Dynamic Fetching of OpenShift Versions

Letting users of Assisted Installer API and UI to choose an OpenShift version
out of a variety of supported versions, all without the need to specifically
configure each of those versions.

## Summary

Currently, we have defined processes to support the growing list of OpenShift
versions. It can be separated to the following cases:
* Adding a newly released minor version of OpenShift when it's first introduced
  as part of an EC (Engineering Candidate), FC (Feature Candidate) or RC (Release
  Candidate) versions. For example, OpenShift 4.12 had its first appearance as
  version ``4.12.0-ec.0``. With the new version we usually also add live-iso
  discovery images that are appropriate for the release.
* Updating beta releases like EC/FC/RC by following candidate channels like
  ``candidate-4.12``.
* Updating to the first official stable version when it's GA (General
  Availability). For example, updating to 4.12.0 once it's published and available
  for users to use.
* Updating stable releases following stable channels like ``stable-4.12``.

All those actions consist of either manual updates by developers or automatic
tools like https://gitlab.cee.redhat.com/assisted-installer-ops/assisted-installer-cicd.
Either manual or automatic, we usually override releases with the new ones (e.g.
replacing 4.12.0 with 4.12.1) to prevent our configuration files from becoming too
large. We retain specific versions as per requests, usually made by
customers/partners.

The idea of this enhancement is configuring the assisted-service to the OpenShift
channels it needs to follow (e.g. ``candidate-4.12``, ``stable-4.11``), and let
it periodically fetch versions to provide them to users.

## Motivation

It becomes a pretty tedious process to handle each version separately. Some
use-cases we tend to have:
* A certain user uses a specific version x.y.z for certification, and we replace
  this version with a newer one x.y.z+w (w>=1). In this case we'll append again the
  x.y.z version to the configuration.
* As described above, we do version updates either automatically or manually.
* QE team wants to check if a newly added OpenShift version has a regression in its
  behavior from a previous version.
* QE team tests a flow which includes OpenShift upgrade process.

### Goals

* Assisted-service is able to provide all versions included in the upgrade-channels,
  based on provided configuration.
* We keep ourselves the option to specifically configure certain versions as we can
  nowadays. This is to make sure we can handle edge-cases and be flexible if needed.
  More about it will be mentioned in the "Risks and Mitigations" section.
* UI needs to be adjusted to handle this growing list of versions (at the time being,
  a total of ~140 versions).

### Non-Goals

* Dynamic handling of y-versions, live-iso discovery images, and such. A new minor
  OpenShift version is introduced every 4 months, requires additional code changes
  (for example in feature-support-level list) and it isn't worth the costs and
  implications of making assisted-image-service refresh itself and pull-in new
  images.
* Handling OpenShift versions in a kubernetes API interface. For this use-case
  versions are externally configured in the ``ClusterImageSet`` custom resource.

## Proposal

The assisted-service will make use of a new configuration variable called
``RELEASE_SOURCES``, which outlines the supported matrix. For example:

```
RELEASE_SOURCES='{
   "4.11": {
      "upgrade_channels": [
         {
            "cpu_architecture": "x86_64",
            "channels": ["stable"]
         }
      ]
   },
   "4.12": {
      "upgrade_channels": [
         {
            "cpu_architecture": "x86_64",
            "channels": ["candidate", "stable"]
         },
         {
            "cpu_architecture": "arm64",
            "channels": ["candidate", "stable"]
         },
         {
            "cpu_architecture": "multi",
            "channels": ["candidate", "stable"]
         }
      ]
   }
}'
```

The assisted-service periodically polls versions from the upgrade-channels API
in the form of ``https://api.openshift.com/api/upgrades_info/v1/graph?channel=<channel>&arch=<arch>``.
For example:
```
$ curl -s 'https://api.openshift.com/api/upgrades_info/v1/graph?channel=stable-4.11&arch=amd64' \
    | jq -r '.nodes[] | select(.version | startswith("4.11")) | .version + " => " + .payload'
4.11.9 => quay.io/openshift-release-dev/ocp-release@sha256:94b611f00f51c9acc44ca3f4634e46bd79d7d28b46047c7e3389d250698f0c99
4.11.4 => quay.io/openshift-release-dev/ocp-release@sha256:e04ee7eea9e6a6764df218bcce3ee64e1816445158a0763954b599e0fc96580b
...
```

When an API consumer (either it's the UI or a direct HTTP consumer) requests
for the available versions via ``/v2/openshift-versions``, versions will be read
from the upgrades-channels API if it weren't queried for a configurable amount
of time (for example, an hour).

To help fetching only certain versions via auto-completion like U/X, we should
provide a parameter ``version_pattern`` for the ``/v2/openshift-versions``
endpoint, along with a ``only_latest`` parameter. For example:
```
/v2/openshift-versions:
   get:
      ...
      parameters:
        - in: query
          name: version_pattern
          description: Version pattern for the returned releases. E.g. '4.12'.
          type: string
          required: false
        - in: query
          name: only_latest
          description: It true, returns only the latest version for each minor.
          type: boolean
          allowEmptyValue: true
          default: false
```

In addition, we should expose a new API to show the current configuration
of dynamic releases for debugging and transparency purposes.

We will still need to handle each new minor version (x.y) by:
* adding RHCOS images for the new version for each supported CPU architecture.
* adding x.y to the new ``RELEASE_SOURCES`` configuration.
* setting the relevant feature-levels in feature-support-levels list.

This way the assisted-service can support each x.y.z version by fetching a
matching x.y RHCOS version from the assisted-image-service. The
assisted-image-service stays up and ready to serve all supported RHCOS images
and the amount of images is only the bare minimum.

### User Stories

#### Installing The Latest Version of a z-stream

As a user of assisted-installer UI, I should have a list of the latest versions
for each minor release.

#### Installing an Older Version of a z-stream

As a user of assisted-installer UI, I should have the option to browse the
different available versions that are fetched from upgrade channels.

#### Configuring the Dynamically-loaded Versions of a Stream

As a system administrator for the assisted-installer, I should have the option
to configure both dynamically-loaded and statically-loaded OpenShift version.
To do that, I can provide the following environment variables for example:
```
RELEASE_IMAGES='[
  {
    "openshift_version": "4.12-multi",
    "cpu_architecture": "multi",
    "cpu_architectures": [
        "x86_64",
        "arm64",
        "ppc64le",
        "s390x"
    ],
    "url": "quay.io/openshift-release-dev/ocp-release:4.12.0-rc.4-multi",
    "version": "4.12.0-rc.4"
  }
]'

RELEASE_SOURCES='[
  {
    "openshift_version": "4.11",
    "upgrade_channels": [
        {
          "cpu_architecture": "x86_64",
          "channels": ["stable"]
        }
    ]
  },
  {
    "openshift_version": "4.11",
    "upgrade_channels": [
      {
        "cpu_architecture": "x86_64",
        "channels": ["candidate", "stable"]
      },
      {
        "cpu_architecture": "arm64",
        "channels": ["candidate", "stable"]
      }
    ]
  }
]'
```

### Risks and Mitigations

We assume here that each x.y RHCOS version is backwards and forwards compatible
with all x.y.z OpenShift versions.

We have recently seen occasions where the disk space is being exhausted due to
the assisted-service caching a lot of different openshift installer binaries.
It is important to make sure we don't make this problem more severe, as this
enhancement introduces much more versions which may (or may not) cause use of
them.
A possible solution here is implementing the cache as a LRU-like cache, making
sure we evict the old entries when we reach a certain capacity.

We should make sure we don't consume too much memory when fetching releases.
It's especially critical when the user queries for the different versions, but
might happen in other cases too.
In case too much versions are getting fetched from the upgrade-channels API,
we should consider limiting the amount of versions used. For example, we can
decide to take only the latest 20 versions of a specified source.

Several cases can require a specific list of images to be configurable out of
existing channels (i.e. the same mechanism we have now with ``RELEASE_IMAGES``).
To name a few:
* There's an image that doesn't exist yet in any upgrade-channel. For example,
  EC images for 4.12 were not initially existing in any channel. Another
  example is images that were removed from a channel but we still need to
  retain because of business requirements.
* We are only interested in one or few images from a channel and wouldn't
  want to add the whole channel. This can apply most for beta versions.
* The upgrade-channels has an outage. In this case the images on
  ``RELEASE_IMAGES`` can act as fallback images that are always available.

## Design Details

### Open Questions

None.

### UI Impact

The UI should prioritize the latest versions when displaying the different
options. Only in need to customize the version (meaning installing an older one),
then it requires clicking on 'Show all available versions' and searching for the
right one.

### Test Plan

A simple e2e test can be made, but it's a bit of waste. The main logic
all happens pre-installation and a little bit when preparing the installation.

For subsystem we can simulate activity with the upgrade-channels API by using
wiremock.

QE test-plan should be fairly simple on the API part - just trying to install
some arbitrary "older" version. When UI gets adjusted to first show latest
versions is where they'll have to change the UI automation.

## Drawbacks

* It means QE no longer verify that each OpenShift version is installable via
  assisted-installer.
* Current proposition doesn't include a mechanism for excluding specific
  versions that are dynamically fetched, in case of critical bugs in them.
  Since it's a low-risk (and low-impact) we can extend it in the future.

## Alternatives

We considered the following options:
* Changing automation of OCP bumps to append versions to configuration
  files (instead of replacement). It was dropped as it creates bloated
  configuration files.
* A major enhancement of this suggestion, where we can dynamically
  track also x.y versions. It got dropped because it requires a lot of
  handling in the image-service to make its availability guaranteed.
