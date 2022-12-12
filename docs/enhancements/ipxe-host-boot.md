---
title: ipxe-host-boot
authors:
  - "@carbonin"
  - “@mhrivnak”
creation-date: 2022-02-22
last-updated: 2022-03-14
---

# iPXE Host Boot

## Summary

Not all user infrastructure is able to boot isos directly, and iPXE is a commonly
used alternative for booting machines. We should support this by making the
Assisted Installer directly serve iPXE artifacts so that systems can boot with no
other iPXE infrastructure required.

## Motivation

Booting a host using iPXE should be a first-class operation when using
assisted-service. This has been found to be at least as common a method
as providing the live ISO via virtual media and is also required in some
environments. Users are already unpacking the ISO we provide into PXE
artifacts and serving those in their infrastructure. We can make this much
easier for them by serving those artifacts as well as the iPXE boot script
directly from our application.

### Goals

- Serve iPXE artifacts (kernel, rootfs, and initrd)
- Serve an iPXE boot script
- Include the ignition in the provided initrd

### Non-Goals

- Support PXE (tftp based)
- Integration with ironic (separate effort)
- iPXE boot script customization (maybe in a follow-up if it’s useful)
- Configure the minimal ISO to pull the rootfs from the image service

## Proposal

The image-service will serve the kernel, rootfs, and initrd with ignition
appended (see Implementation Details), while the assisted service will
provide an API to download the boot script. The boot script created and served
by the assisted service will contain URLs to the required artifacts hosted by
the image service.

### User Stories

#### Story 1

As a user of assisted installer I want to boot my hosts using iPXE without
setting up my own http server to host artifacts.

### Implementation Details/Notes/Constraints

#### Ironic Integration

To support integration with Ironic’s existing iPXE boot methods, the
discovery ignition must be appended to the initrd rather than specified as
a separate kernel parameter. The image service will also handle this initrd
manipulation when an initrd is requested for a particular infra-env.

#### New Image Service APIs

Image service APIs will be added to download the required boot artifacts. The
rootfs and kernel will be served directly from the template image. The initrd
endpoint will be specific to an infra-env in order to embed the ignition.

- `GET /boot-artifacts/{artifact-name}?{params}`
  - artifact-name can be either “rootfs” or “kernel”
  - params must include “version” and “arch” which are used to identify a template image
  - No authentication is required as we’re serving these directly from the uncustomized image
  - Examples
    - `/boot-artifacts/rootfs?version=4.9&arch=x86_64`
    - `/boot-artifacts/kernel?version=4.10&arch=arm64`
- `GET /images/{image-id}/pxe-initrd?{params}`
  - image-id is the uuid of an infra-env in assisted-service
  - params must include “version” and “arch”
  - params may include “api_key” or “image_token” depending on assisted-service authentication configuration
  - Example
    - `/images/afe293b9-e3a0-47ed-8952-f26721626497/pxe-initrd?version=4.10&arch=x86_64&image_token={token}`

#### New Assisted Service REST APIs

Assisted service APIs will be added to download the ipxe boot script and to
retrieve a presigned url for downloading the boot script.

- `GET /v2/infra-envs/{infra-env-id}/downloads/files?file_name=ipxe-script`
  - Will return an iPXE boot script with artifact URLs pointing to the image service
  - Initrd URL will be presigned
- `GET /v2/infra-envs/{infra-env-id}/downloads/files-presigned?file_name=ipxe-script`
  - Will return a presigned URL for the iPXE script

#### Assisted Service Kubernetes APIs

A presigned download url for the iPXE boot script and all relevant artifacts will be added
to the InfraEnv resource status. This URL will be set as a part of the InfraEnv controller
reconcile loop.

Proposed API addition example:

```yaml
status:
  bootArtifacts:
    initrd: https://image-service.example.com/images/afe293b9-e3a0-47ed-8952-f26721626497/pxe-initrd?version=4.10&arch=x86_64
    rootfs: https://image-service.example.com/boot-artifacts/rootfs?version=4.10&arch=x86_64
    kernel: https://image-service.example.com/boot-artifacts/kernel?version=4.10&arch=x86_64
    ipxeScript: https://assisted-service.example.com/v2/infra-envs/afe293b9-e3a0-47ed-8952-f26721626497/downloads/ipxe-script
```

#### Authentication Tokens

Both the iPXE boot script and the initrd download URLs will be presigned with an
authentication token. In the cloud offering (console.redhat.com) the same mechanism
described in image-service-cloud-authentication will be used which means the tokens
will include an expiration.

This expiration will be set to four hours for both the iPXE boot script and the initrd.
The expected use case will be for users to point their machines directly at the boot
script URL which can be refreshed by the UI easily.

This only becomes a potential problem if a user downloads and hosts the iPXE script, but
not the initrd. This is because the URL in the boot script for the initrd will expire after
four hours and machines will no longer be able to boot. In this case the expectation is that
such a user would download all the artifacts, host them in their own infrastructure, and edit
the boot script accordingly.

#### Proxy Support

It should be noted explicitly that iPXE does not have support for proxy servers.
This means that if a host needs a proxy to access the boot artifacts the artifacts
will need to be mirrored and hosted internally.

This should be called out in the UI as the assisted installer otherwise works well
by just specifying proxy information.

### Risks and Mitigations

## Design Details [optional]

### Open Questions

### UI Impact

Is the UI going to display the boot script or just provide the URL?

### Test Plan

## Drawbacks

## Alternatives
