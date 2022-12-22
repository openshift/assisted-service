---
title: short-image-urls
authors:
  - "@mhrivnak"
creation-date: 2022-11-30
last-updated: 2022-12-22
---

# Short image URLs

## Summary

The assisted-image-service should be enhanced to serve discovery ISOs from URLs
that are less than 255 characters and don't have query parameters. This will
enable servers from multiple vendors to directly boot from an
assisted-image-service URL using the baseboard management controller.

## Motivation

Servers from multiple vendors have limits to the length and composition of URLs
from which their BMC can directly boot the system. But there is growing
interest in the use case of directly booting such systems from a URL served by
assisted-image-service, whether on-prem or from api.openshift.com.

### Goals

* Enable servers to boot discovery ISOs directly from a signed URL.

### Non-Goals

* 

## Proposal

### User Stories

#### Story 1

As the operator of bare metal servers, I can boot them from a signed URL served
at api.openshift.com.

### Implementation Details/Notes/Constraints [optional]

#### Requirements

The latest vendor to request this has these requirements:
* no URLs longer than 255 characters
* no query parameters
* no HTTP redirects
* URL must end in ".iso"

Those requirements appear similar to other vendors with limitations.

#### signed URLs

Today's signed URLs look like this:

```
https://api.openshift.com/api/assisted-images/images/739f38a1-5963-4511-8b5f-3c870a2c388c?arch=x86_64&image_token=<TOKEN>&type=minimal-iso&version=4.11
```

That should change to this:

```
https://api.openshift.com/api/assisted-images/bytoken/<TOKEN>/4.11/x86_64/minimal.iso
```

Because the infra-env ID is embedded in the token (which is a JWT), the
infra-env ID can be removed from the URL path.

#### unsigned URLs

Today's unsigned URLs look like this:

```
http://api.openshift.com/api/assisted-images/images/881dd303-940e-4c1a-a844-dc3c64da0e95?arch=x86_64&type=full-iso&version=4.11
```

That should change to:

```
http://api.openshift.com/api/assisted-images/images/881dd303-940e-4c1a-a844-dc3c64da0e95/4.11/x86_64/full.iso
```

#### Token Parsing

Unfortunately the JWT payload uses different keys to identify the InfraEnv
depending on whether the service is deployed as part of OCM
(console.redhat.com) or locally using "local auth".

Tokens served by OCM have a JWT payload with two fields:

```
{
  "exp": 1671661677,                             # expiration in seconds since epoch
  "sub": "e1bfd718-7ce4-4338-a123-07e68214734a"  # infraenv ID
}
```

Tokens served when using local auth have a JWT payload with one self-evident field:

```
{
  "infra_env_id": "5b6e5a9e-9fe2-45a0-a95f-976ee62169aa"
}
```

The image service will need to parse the payload and look for the value under
both keys.

#### Compatibility

The previous URL layouts should continue to work. But the default URLs returned
as part of the InfraEnv resource should be in the new formats, since they are
compatible with a wider range of clients.

### Risks and Mitigations

None

## Design Details [optional]


### Open Questions


### UI Impact

None.

### Test Plan

The tests that exist today should be expanded to cover the new URL formats.

## Drawbacks

?

## Alternatives

Use of a proxy or local mirror is an alternative. For example, the ironic
project serves ISOs to servers by creating a unique (and relatively short) URL
for each host. But this requires an extra component to run as a proxy or
mirror.
