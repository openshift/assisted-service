---
title: extract-coreos-image-from-release
authors:
  - "@danielerez"
creation-date: 2022-06-07
last-updated: 2022-06-07
---

# Extract CoreOS image from OCP release payload

## Summary

New OCP release images (>=4.11) now contain a reference to the correlated CoreOS ISO image in the release payload: ‘machine-os-images’. See: coreos-image-in-release [enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/baremetal/coreos-image-in-release.md).
We would introduce a means in the [assisted-image-service](https://github.com/openshift/assisted-image-service) to extract the OS image from new release images instead of using the specified OS_IMAGES, while keeping the current behavior for old versions (<4.11).

## Motivation

Currently, there is a requirement to specify a [list](https://github.com/openshift/assisted-service/blob/master/data/default_os_images.json) of OS images that are served on an external server. This is an issue for disconnected environments as the CoreOS and Rootfs images must be manually mirrored and deployed to an http server. Mitigating the issue can be done by extracting the OS image from the release. Which means that in disconnects environments only the release payload should mirrored, as already done in these envs with [oc-mirror](https://docs.openshift.com/container-platform/4.10/installing/disconnected_install/installing-mirroring-disconnected.html#installing-mirroring-disconnected).

### Goals

- Introduce OS image extraction support in assisted-image-service (for releases >= 4.11).
- Keep the current behavior support for older releases.

### Non-Goals

- Change the current implementation of pull-secret handling in assisted-service. I.e. the service should still use the cluster's pull-secret for fetching images. See 'Pull-Secret Handling' section.

## Proposal

The assisted-image-service requires a [list] (https://github.com/openshift/assisted-service/blob/master/data/default_os_images.json) of OS images specified in OS_IMAGES env var. Each item in the list represents a specific OCP version and contains URLs for a CoreOS image and a Rootfs image. On image-service startup, these images are download and cached for being served as discovery ISO. This behavior should be should remain unchanged for older OCP versions (<4.11).

For newer versions (>=4.11), on image-service startup, it would instead extract the OS image from the release. This is done by first fetching a reference to the image in the release payload, by using:
`$ oc adm release info --image-for=machine-os-images <release-image>`
Then, extract the image using this reference and save in the image store of the image-service:
`$ oc image extract <image reference> --registry-config <pull-secret> --path /:<store>`

As the image-service currently maintains only a list of OS images (OS_IMAGES env var), another list for release images should be introduced. So for disconnected environments, the list should contain references to the mirrored release images as done using [oc-mirror](https://docs.openshift.com/container-platform/4.10/installing/disconnected_install/installing-mirroring-disconnected.html#installing-mirroring-disconnected).

To use `oc image extract` a pull-secret is required, thus, the image-service should support handling a PULL_SECRET env var which is specified by the deployment (see 'Pull-Secret Handling' section).

As for minimal ISO support, the current implementation is using an external URL to a rootfs image in the grub config. Since we now want to avoid specifying any external URLs, this flow should be modified to refer to the /boot-artifacts API instead. This API in the image-service already can serve the rootfs image directly from the OS image.

### User Stories

#### Story 1

As an AI user deploying a disconnected on-prem environment, I want to avoid mirroring and serving OS images. Instead, rely only on the mirrored OCP release payload used for images in RELEASE_IMAGES list.

### Implementation Details/Notes/Constraints

#### Changes in assisted-image-service

- Support a new env variable: RELEASE_IMAGES.
  - Should be a list of OCP release images, and be in similar format to OS_IMAGES
  - See [default_release_images.json](https://github.com/openshift/assisted-service/blob/master/data/default_release_images.json)
- Retrieve the 'system' pull-secret and cache in memory (see 'Pull-Secret Handling' section).
- On service startup, the following should be invoked for every release image:
  - Fetch `machine-os-images` URI from the release.
    E.g.
    `$ oc adm release info --image-for=machine-os-images quay.io/openshift-release-dev/ocp-release:4.11.0-0.nightly-multi-2022-04-18-120932-x86_64`
    `(output) quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:0fc11beeab34f7187f6dc38ae60fb8e3e9d52d04a9a62d9f4aafe7192a7a83c1`
  - Extract OS image from the fetched `machine-os-images`.
    E.g.
    `$ oc image extract quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:0fc11beeab34f7187f6dc38ae60fb8e3e9d52d04a9a62d9f4aafe7192a7a83c1 --registry-config ~/.pullsecret --path /:/tmp/iso`

* Populate the image store with the extracted OS image (i.e. cache the image).

- The create minimal ISO template flow should inject to the grub configs a reference to /boot-artifacts image-service API (instead of an external URL to rootfs).

#### Pull-Secret Handling

When using oc to extract an image, a pull-secret is required. Therefore, we need to provide it in each deployment using PULL_SECRET env var.

- SaaS: the pull-secret of the service account used for OCM communication can be fetched using AMS [access_token API](https://api.openshift.com/?urls.primaryName=Accounts%20management%20service#/default/post_api_accounts_mgmt_v1_access_token). The pull-secret can be then stored in a vault and used as a vault-secret for the image-service consumption. We probably should add some automation for fetching the pull-secret from AMS and update it in the vault (in case it gets invalidated).
- On-Prem: the user should specify a 'system' pull-secret that would be available on image-service startup (i.e. instead of providing it only when creating a cluster). It should be added to podman deploy [configmap](https://github.com/openshift/assisted-service/blob/master/deploy/podman/configmap.yml).
- Operator: the operator admin should specify a 'system' pull-secret in [AgentServiceConfigSpec](https://github.com/openshift/assisted-service/blob/8f7bc60886cbab86104063d54d2a0e99fd538734/api/v1beta1/agentserviceconfig_types.go#L54).
- CI: the pull secret can be retrieved from the release infra (as done for [test-infra](https://github.com/openshift/release/blob/0e401d7e2fac1aa2beb84b4f9787f385897218cf/ci-operator/step-registry/assisted/baremetal/setup/assisted-baremetal-setup-commands.sh#L94)).

#### Changes in the Operator

As release images are supplied by specifying them in ClusterImageSet CRDs, we need a mechanism to notify the image-service on changes. This is required for the image-service to cache the OS images correlated with the releases.

- This can be done by adding a watch for ClusterImageSets. When one is added/deleted, the image-service deployment should reflect it by getting an updated release images list (RELEASE_IMAGES env var). I.e. the operator should handle a change in release images list and update the relevant deployments accordingly.

### UI Impact

There should be no impact for UI flows.

### Test Plan

- For OCP releases >= 4.11:
  - In all deployments, only a release image is required for a successful cluster installation.
  - Both full and minimal ISO work as expected.
- For older releases - same as current behavior.

## Drawbacks

- Release images are not used by HyperShift. I.e. we must keep the current OS images list in assisted-image-service.

- For backwards compatibility, the current behavior must be maintained as well.

- The work done here for the operator handling would be redundant if we support adding OCP releases versions dynamically in SaaS later. As it means that we would have to support on-demand image handling in the image-service instead of only at startup.

- This effort facilitates the deployment of disconnected environment only. Won't substantially assist with SaaS.

## References

- [Epic](https://issues.redhat.com/browse/MGMT-8581): Extract OS image from release payload
