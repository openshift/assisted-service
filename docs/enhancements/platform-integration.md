---
title: Platform-Integration
authors:
  - "@vrutkovs"
creation-date: 2022-07-26
last-updated: 2022-07-26
---

# Platform integration in Assisted Installer

## Summary
Assisted installer has historically been used to configure baremetal clusters. Eventually, users have grown accustomed to use it for virtual machines in the cloud, as it helped them to validate networking settings. The installed cluster however could not make use of cloud features (like scaling, volumes or loadbalancers) as it was installed with platform: none. Later on basic platform integration was added, described in [Define Providers Interface](https://github.com/openshift/assisted-service/blob/master/docs/enhancements/add-external-providers.md).

This enhancement extends providers API and describes how other components are interacting with this feature.

## Motivation

Adding other providers for integration with assisted installer should be easy and customizable, including various UX/UI changes for flexibility.

### Goals

- Define API changes necessary to support various providers
- Define API for UI to allow being flexible about provider features
- Define requirements for other components - most importantly, openshift-installer - required to be fulfilled to allow assisted-installer team add more integrations in the future.
- Convert existing integration implementations to use new APIs

### Non-Goals

- Integrating any relevant [Openshift installer types](https://github.com/openshift/installer/tree/master/pkg/types) automatically - this would require vendoring a particular openshift-installer version, while assisted-installer aims to support multiple versions

## Proposal

- Extend [feature-support-levels](https://github.com/openshift/assisted-service/blob/master/swagger.yaml#L3561-L3599) to accept provider parameter:
```yaml
  feature-support-level:
  ...
            feature_id:
              type: string
              description: The ID of the feature
              enum:
              - `ENABLE_CNV`
              - `ENABLE_ODF`
              - `ENABLE_SCHEDULABLE_MASTERS`
              - `ENABLE_DAY2_HOSTS`
              - `ENABLE_NETWORK_TYPE_SELECTION`
              - `ENABLE_CUSTOM_MANIFEST`
              - `ENABLE_USER_MANAGED_NETWORKING`
              - 'ADDITIONAL_NTP_SOURCE'
              - 'REQUESTED_HOSTNAME'
              - 'PROXY'
              - 'SNO'
  ...
```
- Rework UI to use `ALLOW_` features - i.e. disable network type selection when `ENABLE_NETWORK_TYPE_SELECTION` is false
- Allow providers list all enabled UI features via `SetPlatformUsages` Provider interface
- `platform_type` feature should be validated by assisted-service against version support matrix (i.e. Nutanix platform available for 4.11+ clusters)
- Allow providers list additional validations to be performed via `SetPlatformUsages` Provider interface
- Document requirements for openshift-installer

### User Stories

#### Story 1

As an Openshift developer working on provider X I want to add support for installing Openshift with assisted installer
on top of platform X.

#### Story 2

As an Openshift developer working on the assisted installer team I want to let providers hook into the
assisted installer flow natural without having to worry about provider specific implementation.

#### Story 3

As an Openshift developer working on the assisted installer UI team I want to use a unified API to enable platform integration feature and use the API to selectively enable or disable features.

### Implementation Details/Notes/Constraints

#### Changes to the assisted-service project:

##### Step 1: Design API for provider-specific features

Extend Usages API to include:
* additional validations
* UI features
* provider's `SetPlatformUsages` should list all enabled UI features.
  If provider doesn't list a feature its condsidered to be disabled.
* Post-install configuration reference (KB, documentation link etc.)


#### Changes to the assisted-ui project:

##### Step 1: Rework UI using API from previous step

UI needs to fetch available features enabled via `features` API and enable/disable elements accordingly

#### Changes to the assisted-agent project:

##### Step 1: Detect provider-specific settings

Providers will list features and additional validations in `SetPlatformUsages`

#### Changes to the Openshift installer project:

##### Step 1: Document requirements for `create manifest` step

In order to avoid storing sensitive information and credentials, providers are configured using stub
data in install-config.yaml. User is expected to update this information after installation, via CLI, console or assisted installer UI addon.

`openshift-install create manifests` runs on assisted-service host, so any requests to provider API will fail.
This step should not require provider API connection to pass, but `create cluster` should be able to.

All validations requiring API access in installer should be moved out of:
* `pkg/asset/installconfig/<provider name>/validation.go`'s `Validate` to `ValidateForProvisioning` function
* `pkg/asset/manifests/infrastructure.go`

### Risks and Mitigations

Implementing provider-specific flows would extend our testing matrix and may confuse users. The mitigation may be additional CI tests and feature-gating UI changes.

## Design Details

### Closed Questions

### UI Impact

UI needs to use `usages` API to selectively enable or disable UI features.

UI should be updated to dynamically display `Integrate with <platform name>` based on API replies

### Test Plan

- Add more unit-tests for proposed API
- Add CI e2e tests for each platform integration

## Drawbacks

## Alternatives
