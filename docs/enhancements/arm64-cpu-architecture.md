---
title: arm64-cpu-architecture
authors:
  - "@danielerez"
creation-date: 2021-07-29
last-updated: 2021-07-29
---

# ARM64 CPU architecture support

## Summary

The default host CPU architecture supported for installation is x86_64 in Assisted Installer. In order to support additional CPU architectures, the assisted-service should accept and handle selection of arm64 CPU architecture as well.

## Motivation

Although x86_64 is popular in many use cases, other CPU architectures are common in other segments. For example, ARM is popular in edge use cases. OpenShift has recently added support for arm64 and it is important to add this support to Assisted Installer.

### Goals

- Allow installation of clusters that are running on arm64 hosts.
- The introduced infrastructure should facilitate future support of more architectures (e.g. PPC).

### Non-Goals

- The assisted-service and assisted-image-service will not be built for arm64.
- No support for clusters with hosts of multiple architectures (e.g. x86_64 + arm64).
- Implementation for kube-api as ClusterImageSet and AgentServiceConfig CRDs. Though the feature design should consider implications.
- Day2 and Late binding won't be supported in first phase.
- On-Prem flows, for now the focus is for SaaS users.
- No baremetal platform support for arm64 in 4.9. I.e. only SNO for now.

## Proposal

### Host images

The following images should also be built for arm64:

- assisted-installer
- assisted-installer-controller
- assisted-installer-agent

This should be handled in similar to the current flows by modifying relevant Dockerfiles to support arm64 build. The images should be published to the same registries and follow same naming conventions. I.e. The best approach would be using the same image URI for any architecture. Which means, minimal changes in the assisted-service, as the architecture specific image would be fetched according to the machine pulling the image.

### OCP versions

[Versions](https://github.com/openshift/assisted-service/blob/master/internal/versions/versions.go) should
have CPU architecture granularity for each API. So that OCP release images and RHCOS images of multiple architectures could be supported.

The assisted-service should support the arm64 variant of RHCOS images and OCP release image (for OpenShift >= 4.9).

As a preparation for supporting multiple CPU architectures in Version, the RHCOS images and release images information from OPENSHIFT_VERSIONS environment variable should be extracted into new variables: OS_IMAGES and RELEASE_IMAGES.
We should probably use a simple array structure instead of mapping, to align with the k8s API convention in [AgentServiceConfig](https://github.com/openshift/assisted-service/blob/77cb9d3348dafd2abd61fd7c8066e7e7a7805d95/config/samples/agent-install.openshift.io_v1beta1_agentserviceconfig.yaml#L18-L22) structure.

To simplify backwards compatibility, we can still support the old format of OPENSHIFT_VERSIONS by keeping the current properties for old versions (<4.9).

#### OPENSHIFT_VERSIONS

This environment variable should be deprecated and kept only for backwards compatibility.
I.e. Instead, RELEASE_IMAGES and OS_IMAGES should be set (if missing, fallback to previous behavior).

#### RELEASE_IMAGES

A list of available release images (one for each minor OCP version and CPU architecture):

```json
[
  {
    "openshift_version": "4.9",
    "cpu_architecture": "x86_64",
    "url": "quay.io/openshift-release-dev/ocp-release:4.9.0-rc.4-x86_64",
    "version": "4.9.0-rc.4"
  },
  {
    "openshift_version": "4.9",
    "cpu_architecture": "arm64",
    "url": "quay.io/openshift-release-dev/ocp-release:4.9.0-rc.4-aarch64",
    "version": "4.9.0-rc.4"
  }
]
```

#### OS_IMAGES

A list of available OS images (one for each minor OCP version and CPU architecture):

```json
[
  {
    "openshift_version": "4.9",
    "cpu_architecture": "x86_64",
    "url": "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/latest-4.9/rhcos-live.x86_64.iso",
    "rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/pre-release/latest-4.9/rhcos-live-rootfs.x86_64.img",
    "version": "49.84.202107032011-0"
  },
  {
    "openshift_version": "4.9",
    "cpu_architecture": "arm64",
    "url": "https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/pre-release/latest-4.9/rhcos-live.aarch64.iso",
    "rootfs_url": "https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/pre-release/latest-4.9/rhcos-live-rootfs.aarch64.img",
    "version": "49.84.202106272247-0"
  }
]
```

### Cluster creation and ISO generation

When creating a new cluster, `cpu_architecture` property should be supported to define which hosts can be registered to it. This property would be added to cluster-create-params and stored in cluster upon creation. If not specified, defaulted to x86_64 as a fallback.

An example for the swagger schema:

```
cpu_architecture:
   type: string
   default: 'x86_64'
   description: The CPU architecture of the image (x86_64/arm64/etc)
```

When generating a discovery ISO, the proper RHCOS/Release images should be used according to CPU architecture specified in the cluster. The `cpu_architecture` property should be stored in `infraenv` after the ISO is generated.

### User Stories

#### Story 1

As an Assisted-Installer user, I need to install a cluster on non x86_64 hosts.

#### Story 2

As an Assisted-Installer user, I need to install a Single-Node OpenShift cluster on an arm64 host.

### Implementation Details/Notes/Constraints [optional]

- The current focus of the enhancement is for SaaS users.

- Only SNO is supported as there's no Baremetal support for arm64 in 4.9 (arm packages for Ironic, etc).

- Late binding is not covered by this enhancement, but decoupling the discovery ISO generation from the cluster should be simple enough. I.e. adding `cpu_architecture` property to `infraenv_create_params` and override the architecture defined in the cluster. Also, architecture validation should be probably added to host discovery conditions (i.e. to ensure that the host CPU architecture is similar to the cluster's architecture).

- When supporting Day2, it should be handled by adding `cpu_architecture` property in `AddHostsClusterCreateParams`, which is needed for generating the proper discovery ISO. We could probably rely on the current validations of host discovery, i.e. the user should ensure architecture when generating the ISO.

- Need to verify IPv6 flows: [OVN-Kubernetes](https://github.com/ovn-org/ovn-kubernetes)

- LSO/OCS/CNV operators: add a validation for x86_64 CPU architecture. See:
  - 'lso-requirements-satisfied'
  - 'ocs-requirements-satisfied'
  - 'cnv-requirements-satisfied'

### Open Questions

#### kube-api considerations

##### ClusterImageSet

Since we extract the openshift-baremetal-install binary in the assisted-service (to create the manifests of the cluster), the x86_64 OCP release image has to be used. Which means that both arm64 and x86_64 release images must be provided. The OCP release image probably won't support the manifest list feature, so we couldn't rely on it either.

Alternatives:

- We could add another URL for the x86_64 release image in ClusterImageSet.
- As a workaround, we could use something like [qemu-user-static](https://github.com/multiarch/qemu-user-static), which enables invocation of multi-architecture containers using QEMU. I.e it might be sufficient just for running the arm64 openshift-baremetal-install binary, though not sure we want to rely on such solution.

For now, we'll try to start with supporting None platform only. So we could use the x86_64 openshift-install binary embedded in the arm release. See: [extract Linux binaries for multiple architectures](https://github.com/openshift/oc/pull/816)

##### AgentServiceConfig

`osImages` property currently contains a single set of images for each OCP version.
E.g.

```
osImages:
- openshiftVersion: '4.8'
  rootFSUrl: https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.8/4.8.2/rhcos-live-rootfs.x86_64.img
  url: https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.8/4.8.2/rhcos-4.8.2-x86_64-live.x86_64.iso
  version: 48.84.202107202156-0
```

We should consider API changes to support multiple architectures for each version. Perhaps just add `cpuArchitecture` property for each image, i.e. multiple images for each version. Would need to modify parsing handling obviously.

### UI Impact

- New cluster dialog:
  - Add 'CPU Architecture' select box.
  - Should be displayed only for versions >= 4.9.
  - Should be disabled on update cluster dialog.
- Generate Discovery ISO:
  - Add an indication for the selected CPU architecture.

### Test Plan

- Need dedicated arm64 machines for running tests in the CI.
- Need to consider which tests to run for arm64 clusters in the CI (using assisted-test-infra). We could probably start with merely the positive flow of full SNO installation, and add tests afterwards if required.

## Alternatives

Rather than explicitly defining CPU architecture in the cluster resource, it can be inferred by the firstly added host. I.e. the first registered host would the determine the cluster's architecture, so any additional hosts would have to validate against that host. That means the architecture should be set only when creating an ISO, which might be clearer to the user. However, the drawback of having validation warnings is probably worse UX any way.
