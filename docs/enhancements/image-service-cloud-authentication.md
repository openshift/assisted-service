---
title: image-service-cloud-authentication
authors:
  - "@carbonin"
creation-date: 2021-10-07
last-updated: 2021-11-15
---

# Image Service Cloud Authentication

## Summary

Authentication between the image service and assisted service when deployed
in the cloud requires a new strategy because the existing tokens used in the cloud
are too large and too short lived to be included in a pre-signed URL. Instead
of the existing authentication used in the cloud, assisted service will issue JWTs
using a symmetric key specific to each infraEnv. This token will be used
as authentication for requests to assisted service, specifically for the endpoints
required by the image service.

## Motivation

When the assisted service is deployed in the cloud, it relies on Red Hat
SSO for user authentication and authentication tokens from Red Hat SSO are not
usable with pre-signed URLs for two main reasons:

- Size - these tokens are typically ~2000 characters long
- Expiration - these tokens have a maximum lifespan of 15 minutes

### Goals

- Define an authentication strategy suitable for pre-signed URLs in the cloud. This includes a
  token with a longer expiration (several hours) and a reasonable size (10s to 100s of characters)
  which will be included directly in a download URL

### Non-Goals

- Tie an image download URL to a Red Hat account or organization
- Maintain public/private key pairs as service configuration to generate and issue tokens
- Add additional external identity management components

## Proposal

The new authentication mechanism will be a JWT signed by the assisted service
using a randomly generated key stored with the infraEnv. The JWT will include
an `exp` claim as defined in RFC7519 and a `sub` claim containing the infraEnv
ID. A token will be included as a parameter in the image download URL and
assisted service authentication will pass if the token in the URL is not expired
and validates using the key in the infraEnv record.

Managing the signed URL and key will require a few API changes. The REST API will no
longer return a `download_url` as a part of an infraEnv as the token in the URL could
be expired at the time of the call. A new API will be added to fetch pre-signed URL
which will include a new token. Another new API will be added to force a new key to be
generated. This will allow a user to invalidate all existing non-expired tokens in case
a download URL is leaked.

### User Stories

#### Story 1

As a user of assisted service via console.redhat.com, I want a simple download
URL to access the discovery ISO. This URL should be usable with standard download
utilities (i.e. `curl`, `wget`) without the need for request headers or additional tools
like the `ocm` CLI.

#### Story 2

As a user of assisted service via console.redhat.com, I want my sensitive credentials
and infrastructure information contained within the discovery ISO to be kept secure.

#### Story 3

As a user of assisted service via console.redhat.com, I want the discovery ISO download
URL to be usable by anyone I choose to give it to without disclosing my personal Red Hat
account credentials or API tokens.

### Implementation Details/Notes/Constraints [optional]

Assisted service will implement a new security definition to serve this purpose.
The existing definitions already have well-defined use cases and scopes that do not
fully match the requirements of this proposal. Specifically the existing urlAuth security
definition applies to more endpoints than we would want to expose for this enhancement.

This new security definition will apply only to the following endpoints:

- `/v2/infra-envs/{infra_env_id}/downloads/files`
- `/v2/infra-envs/{infra_env_id}/downloads/minimal-initrd`

These are the endpoints that the image service uses to fetch image customization information.

The symmetric key will be stored in a new column in the `infra_envs` database table and will not
be accessible through the infraEnv API. The expiration time for tokens will be 4 hours by default
(the same as the current image expiration time), but will also continue to be configurable.

New API endpoints:

- `GET /v2/infra-envs/{infra_env_id}/downloads/image-url`
  - Get a new pre-signed download URL for an infraEnv's discovery image
  - Example response:
    - `{"url": "https://image-service.example.com/images/{infra_env_id}?image_token=<some-jwt>&version=4.9&arch=x86_64&type=full-iso", "expires_at": "2021-11-15T21:36:46.000Z"}`
- `POST /v2/infra-envs/{infra_env_id}/regenerate-signing-key`
  - Create a new symmetric key for an infraEnv (invalidates all existing JWTs)

These new endpoints and will be protected by SSO user credential authentication and authorization.

The image service will accept a new URL parameter, `image_token`, which will
then be forwarded to assisted service in the `Image-Token` header key.

#### Assisted Service Authentication and Authorization Flow

1. Request arrives to one of the infraEnv download endpoints with an `Image-Token` header
2. Assisted service middleware stores infraEnv ID from the request path in the request context
3. Authentication validates the token using the key associated with the infraEnv for the id in the token `sub` claim
4. If a token is valid, authentication stores the `sub` claim in the request context
5. Authorization ensures the infraEnv ID from the `sub` claim in the request context matches the infraEnv ID in the request path (also from the context)
6. Request is processed

### Risks and Mitigations

Accessing a database during authentication has a performance impact, but we should be able
to cache keys by infraEnv id which would mitigate most of this. Additionally we already
check the database for authorization and the added time to fetch the key should be small
compared to the total download time so it likely won't be something users notice.

Introducing a new authentication system always involves some risk that it will be implemented
incorrectly, but this addition should be easy enough to understand and address any issues that
could come up.

## Design Details [optional]

### Open Questions

- Should we also support RHSSO user tokens in a header for these endpoints?
  - The image service could also pass a header through the assisted service

### UI Impact

The UI is currently reading the image download URL from the infraEnv. This will need to change
to request the URL separately after the infraEnv is created.

### Test Plan

- Image downloads from the cloud with the image service and the new authentication work correctly.
- An image can not be downloaded without a token or with an invalid token.
- The `image-url` endpoint returns a URL with a new token with a new expiration claim.
- The regenerate signing key endpoint invalidates all existing tokens (requests no longer succeed with any previously generated URL).
- URLs with an existing token expire after 4 hours

## Drawbacks

Implementing our own authentication system is generally risky. We already have a system
(RHSSO) that works in the cloud, gives us authorization as well, and we don't maintain.
The only reason we can't use this system for this use case is that we want to make
the download easier for users. The most secure, and most flexible option would be
to require an OCM JWT token for this download, just as we do for every other API call.

## Alternatives

- Use RHSSO and a token in the header as we do for all other API requests
  - Discussed in "Drawbacks", but this would be much less work and one less authentication
    system to maintain.
  - Downsides are UX
