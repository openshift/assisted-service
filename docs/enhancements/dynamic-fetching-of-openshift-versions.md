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
  as part of an EC (Engineering Candidate), FC (Feature Complete) or RC (Release
  Complete) versions. For example, OpenShift 4.12 had its first appearance as
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
``DYNAMIC_OPENSHIFT_RELEASES_CONFIG``, which outlines the supported matrix. For
example:

```
DYNAMIC_OPENSHIFT_RELEASES_CONFIG='{
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

The assisted-service periodically polls versions from the ``upgrades_info`` API
in the form of ``https://api.openshift.com/api/upgrades_info/v1/graph?channel=<channel>&arch=<arch>``.
For example:
```
$ curl -s 'https://api.openshift.com/api/upgrades_info/v1/graph?channel=stable-4.11&arch=amd64' \
    | jq -r '.nodes[] | select(.version | startswith("4.11")) | .version + " => " + .payload'
4.11.9 => quay.io/openshift-release-dev/ocp-release@sha256:94b611f00f51c9acc44ca3f4634e46bd79d7d28b46047c7e3389d250698f0c99
4.11.4 => quay.io/openshift-release-dev/ocp-release@sha256:e04ee7eea9e6a6764df218bcce3ee64e1816445158a0763954b599e0fc96580b
...
```

It then adds any new version found by this process in the database. When a
consumer of the API requests the available versions via ``/v2/openshift-versions``
(either it's the UI or a direct REST request) versions are read from the
database.

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
* adding x.y to the new ``DYNAMIC_OPENSHIFT_RELEASES_CONFIG`` configuration.
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

DYNAMIC_OPENSHIFT_RELEASES_CONFIG='[
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

We need to think of the retention policy of versions. We don't expect to have
versions removed from the upgrade-channels API, but just in case we should only
add versions and not mirror exactly the given result of those API responses.
As opposed to that, when a minor release gets EOL (end-of-life) and we remove it
from the configuration, the service should remove all the stale versions from
the database.

## Design Details

### Open Questions

* Is the database usage a real necessity? The idea is having the periodic thread
  updating the database, and then the HTTP server is able to filter versions from
  the database at its leisure. If there's a good way to do it in-memory and it
  doesn't consume too much of it, it might be better.
* When implementing the binaries LRU cache, should we prioritize the latest
  versions of each y-release? We expect those to be the most common ones, but
  maybe it's better to let the LRU mechanism determine what are the common
  versions by itself?
* How should we limit database entries? In case of a bug in the code that writes
  way too much version entries, can we make sure to mitigate the issue on our
  side? (without interrupting app-sre for their help on manual database
  intervention)

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
