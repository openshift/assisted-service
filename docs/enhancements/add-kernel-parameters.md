---
title: add-discovery-kernel-arguments
authors:
- "@oamizur"
  creation-date: 2022-09-20
  last-updated: 2022-11-11
---

# Add discovery kernel arguments

## Summary

Some environments require additional kernel arguments to boot host machines.
We would like to support additional kernel arguments while booting the discovery image.

## Motivation

Previously, adding kernel arguments was required by several customers in order to adapt to various environments.
For example **rd.net.timeout.carrier** may be required in order for either static networking to be applied or
to successfully get a dhcp address before pulling the rootfs.

### Goals

Add capability to manipulate kernel arguments in the following ways:

- Append: Append new argument to the existing argument set
- Replace: Replace an existing kernel argument with a new value
- Delete: Delete a kernel argument from the existing argument set

Image types: Full ISO, minimal ISO, iPXE

Deployment types: Cloud, operator

### Non-Goals

## Proposal

The discovery kernel arguments will be stored in the infra-env database record in a designated JSON encoded field.
During infra-env registration and update the full set of the kernel arguments can be set.
Since the kernel arguments are defined at infra-env level, if different set of arguments is needed for
different hosts, then different infra-env is needed (one per kernel arguments set).

Setting the arguments can either be done by either REST API or Kube API (Part of InfraEnv CR)

### User Stories

#### Story 1

When having slow NIC, increase the timeout to have the IP address configured.

### Implementation Details/Notes/Constraints

#### iPXE implementation

iPXE implementation should be quite straight forward. The arguments will be concatenated at the end of the
**kernel** command which is part of the iPXE script.

#### Minimal and full ISO

When streaming the discovery ISO file, the kernel arguments are included
as part of some files: grub.cfg and isolinux.cfg

Each of these files contains an area where the kernel arguments can be
embedded. This can be identified by the regex **"(\n#+) COREOS_KARG_EMBED_AREA"**.
The arguments should overwrite this are from the beginning and should
not pass the last # (hash sign). The added kernel will be injected in the beginning.
These arguments will be seperated by space **(" ")**. When embedding the kernel arguments,
the size of the above files will not change.

Starting from version 4.10, there is a file that contains the file names,
while in older versions the file does not exist. If the file exists, the
files to embed the kernel arguments are taken from this config file.
Otherwise, the default files **[/EFI/redhat/grub.cfg, /isolinux/isolinux.cfg]**
are used.

We will need the image service to query the assisted service to get the kernel arguments.
This will require authentication. The authentication methodology may have 2 variants:

1. When running in cloud
2. When running in disconnected environment

The image service already queries the assisted service. We will use the same authentication
methods. We will need to verify that the infra-env endpoint may be accessed using the same authentication
mechanism.

#### UI

Eventually, UI may support kernel arguments modification in all places that infra-env is manipulated.

#### Kube API

The discovery kernel arguments should be defined in the spec section in InfraEnv CR.
They will have the following format:

In InfraEnv Spec section:

```sh
KernelArguments []KernelArgument `json:"kernelArguments,omitempty"`
```

In addition there is the definition of **KernelArgument**:

```sh
type KernelArgument struct {

	Operation string `json:"operation,omitempty"`

	Value string `json:"value,omitempty"`
}
```

### Risks and Mitigations

## Design Details [optional]

### Open Questions

Coreos Installer already supports the operations mentioned in this document but does not
have full-support for streaming. Once it will have such support, the image service will move to
use the Coreos Installer. It will be done not as part of the effort to add discovery kernel parameters.

Here is a link to the Coreos Installer kernel arguments modification reference:
https://coreos.github.io/coreos-installer/cmd/iso/#coreos-installer-iso-kargs-modify

### UI Impact

### Test Plan

## Drawbacks

## Alternatives
