---
title: Define-Providers-Interface
authors:
  - "@Gal-Zaidman"
creation-date: 2021-07-25
last-updated: 2021-10-04
---

# Define Providers Interface

## Summary

Currently, the assisted installer enables you to install on any platform where you can boot the discovery ISO
resulting in a cluster with platform set to none, and also supports vSphere and Bare-Metal platforms.

Openshift can also be installed on other on-prem providers such as:

- Red Hat Virtualization (oVirt).
- OpenStack.

Currently, the assisted installer supports only the Bare-Metal and vSphere platforms, but for future on-prem
providers we need to make sure there is a clear interface each on prem provider can implement to add its platform.

This enhancement will define a clear interface to extend the assisted installer to add on prem provider integrations.

## Motivation

Assisted Installer can be a great tool to perform User Provisioned installation (UPI) installation on external providers,
providing the user with clear UI to see its provisioned machines joining the cluster, and lowering installation
resources requirements.
It is only natural that on-prem providers will want to follow vSphere path and extend assisted installer to support
their platform, to make sure that the provider specific code is easy to maintain, doesn't burden the assisted installer team,
and the providers have a clear understanding of what they need to implement to add support to their platform we need to
define a clear interface which each provider can implement and maintain.

### Goals

- Define a clear way to add a specific platform provider to the assisted installer.
- Migrate all existing supported providers to implement the same unified interface - and test it with assisted-test-infra.

### Non-Goals

- Support IPI installations.
- Integrating any relevant [Openshift installer types](https://github.com/openshift/installer/tree/master/pkg/types)
  into the assisted installer code - this will require a large refactor of the assisted installer code.
- Remove "Baremetal" or "BM" words from inappropriate struct/function names. (as this is how the assisted installer was originally implemented)
- Adding Support for new providers.

## Proposal

- Create a Provider interface, and hooks in the assisted installer code which separates the assisted installer core code
  from the provider-specific code.
- Add clear documentation for each provider specific function.
- Document steps to add a provider to the assisted installer.

### User Stories

#### Story 1

As an Openshift developer working on provider X I want to add support for installing Openshift with assisted installer
on top of platform X.

#### Story 2

As an Openshift developer working on the assisted installer team I want to let providers hook into the
assisted installer flow natural without having to worry about provider specific implementation.

### Implementation Details/Notes/Constraints

#### Changes to the assisted-service project:

##### Step 1: Detect platform specific logic

Go over the assisted installer flow and detect parts of the code which require provider specific logic.
This step is based mostly on the work that already been done on the vSphere provider addition (MGMT-7067).

##### Step 2: Create a Go interface

We should create a Go interface that will contain each provider specific logic in a function
for each provider to implement.

Prototype:

```go
// Provider contains functions which are required to support installing on a specific platform.
type Provider interface {
    // Name returns the name of the platform.
    Name() models.PlatformType
    // AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
    // sets platform fields from values within the cluster model.
    AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
    // CleanPlatformValuesFromDBUpdates remove platform specific values from the `updates` data structure
    CleanPlatformValuesFromDBUpdates(updates map[string]interface{}) error
    // SetPlatformUsages uses the usageApi to update platform specific usages
    SetPlatformUsages(platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
    // IsHostSupported checks if the provider supports the host
    IsHostSupported(hosts *models.Host) (bool, error)
    // AreHostsSupported checks if the provider supports the hosts
    AreHostsSupported(host []*models.Host) (bool, error)
}
```

##### Step 3: Implement the provider registry

Implement the registry pattern so providers can register with their names:

```go
type Registry interface {
   // Register registers a provider.
   Register(provider provider.Provider)
   // Get returns a provider registered to a name.
   // if provider is not registered returns an ErrNoSuchProvider
   Get(name string) (provider.Provider, error)
}
```

Implement the provider registry that will act as an API to all the implemented providers, it will contain alll the metods:

```go
type ProviderRegistry interface {
   Registry
   // GetSupportedProvidersByHosts returns a slice of all the providers names which support
   // installation with the given hosts
   GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error)
   // AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
   // sets platform fields from values within the cluster model.
   AddPlatformToInstallConfig(p models.PlatformType, cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
   // SetPlatformUsages uses the usageApi to update platform specific usages
   SetPlatformUsages(p models.PlatformType, platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
   // IsHostSupported checks if the provider supports the host
   IsHostSupported(p models.PlatformType, host *models.Host) (bool, error)
   // AreHostsSupported checks if the provider supports the hosts
   AreHostsSupported(p models.PlatformType, hosts []*models.Host) (bool, error)
}
```

##### Step 4: Add the provider registry to relevant structures and call it

We should add the provider registry to structs that needs to call it on their initialization and
call the relevant function(create a hook) at the appropriate places.

##### Step 5: Migrate existing provider code to new Provider interface

Make sure all the existing provider specific(baremetal/vsphere) code is ported to the new interface.

##### Step 6: Adjust existing/add unit tests

Make sure all the existing test cases work and add new tests if required.

#### Changes to the assisted-agent project:

After the agent is running on the host, the provider should be determined from the OS details of the node.
The [GetVendor](https://github.com/openshift/assisted-installer-agent/blob/master/src/inventory/system_vendor.go#L29-L45)
method calculates if a node is virtual or not and populates the SystemVendor model which is later being used by the
assisted service to determine if a node is running on a certain provider.
We need to make sure that the provider is listed in the
[isVirtual](https://github.com/openshift/assisted-installer-agent/blob/master/src/inventory/system_vendor.go#L15-L19]
list and that it can be detected using the fields in
[SystemVendor](https://github.com/openshift/assisted-service/blob/master/models/system_vendor.go).

See [PR](https://github.com/openshift/assisted-installer-agent/pull/225) as an example.

#### Changes to the Openshift installer project:

A small change is required in the installer project.
Since the openshift installer is generating a random InfraID the names of the Nodes(in the manifest) are different
from the nodes the user provisions, and since we have a provider set then the provider will try to start new machines
due to the naming mismatch.
Since the InfraID is random we can't tell the user to create the Nodes with specific names.
Also, the InfraID is later used by the platforms in various ways to identify the cluster,
for example in oVirt we use it to set an oVirt tag which helps us group the cluster resources in oVirt,
and we rely on it in various places such as cluster destroy or CSI/Machine provider logic to filter cluster resources
quickly and prevent unnecessary API calls.

We need to add an env var to allow us to override the generated InfraID.
We started a PR on the [installer project](https://github.com/openshift/installer/pull/5101).

### Risks and Mitigations

As with every large code change, this change contains the risk of breaking existing functionality.
This can be mitigated by sanity testing and making sure each provider implements unit tests and e2e test suite.

## Design Details

### Closed Questions

> 1. Throughout the assisted installer code we have structs and functions that are specific to baremetal(at least from the name),
>
> - Are they safe to be extended to support providers?

Yes, of course they need to be extented without breaking anything.

> - Is there anything baremetal specific we should be concerned about?

No

> - Is the assisted installer team aims to modify the names to be generic? for example I don't see why the
>   [InstallerConfig](https://github.com/openshift/assisted-service/blob/master/internal/installcfg/installcfg.go#L75-L110)
>   struct is BareMetal.

No plans but we can modify them if it make sense as we develop.

> Some examples but it is on every file:
>
> - [BMACReconciler](https://github.com/openshift/assisted-service/blob/master/internal/controller/controllers/bmh_agent_controller.go#L50-L55)
> - [bareMetalInventory](https://github.com/openshift/assisted-service/blob/master/internal/bminventory/inventory.go#L149-L175)
> - [InstallerConfigBaremetal](https://github.com/openshift/assisted-service/blob/master/internal/installcfg/installcfg.go#L75-L110)
> - [bmhIsMaster](https://github.com/openshift/assisted-service/blob/master/internal/ignition/ignition.go#L541-L551)

> 2. Is there a use case in which the user will provision the Nodes on a certain provider but want to disable
>    provider integration? meaning nodes will be provider will be discovered but we want platform none anyway?
>    in which case what would be the best way to handle this in terms of user interaction?
>    would an env var flag to disable provider integration is enough?

We have to support that use case, currently the selection is done in the UI, this will be answered per provider
implementation and it is out of scope for this enhancement

### UI Impact

Currently, no UI changes are required.
Each provider added should consider UI changes like vSphere(MGMT-7102)
but this is out of scope for this enhancement

### Test Plan

- Each provider will be in charge of implementing it's own e2e test suite.
- Each provider QE team will be in charge of the testing.

## Drawbacks

## Alternatives
