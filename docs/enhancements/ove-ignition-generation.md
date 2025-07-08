---
title: ove-ignition-generation
authors:
  - "@ovishlit"
creation-date: 2025-07-15
last-updated: 2025-07-15
---

# OVE Ignition Generation

## Summary

Enable serving OpenShift Virtualization Edition (OVE) ISOs with custom ignition containing user-provided values (SSH keys, pull secrets). OVE ISOs are 40GB+ self-contained images that include the entire OpenShift release payload, operators, and custom images for a specific version, designed for disconnected environment deployments. 

This enhancement allows downloading OVE ISOs with pre-configured values embedded in the ignition, enabling a more streamlined deployment process while maintaining compatibility with the existing interactive flow. The downloaded ISO becomes user-specific, with its ignition constructed from unconfigured ignition, and user-specified values from SaaS.

## Motivation

OVE ISOs are designed to facilitate OpenShift deployments in disconnected environments by being completely self-contained. The current workflow requires two separate phases:

1. **Above-sea-level** (SaaS UI): Users download a generic OVE ISO
2. **Below-sea-level** (Post-boot): Users manually configure essential settings through an interactive GUI

This separation creates several challenges:
- Manual intervention is required for every deployment
- Automation of OVE-based installations is not possible
- Configuration errors can occur during manual entry
- The deployment process is slower and more error-prone

By allowing users to specify configuration values in the SaaS UI (above-sea-level) before downloading the ISO, we merge these two phases. Users can provide their SSH keys and pull secrets, and the resulting ISO contains these pre-configured values, eliminating the need for manual post-boot configuration while still preserving the option to use the interactive flow for additional configuration if needed.

### Goals

- Enable downloading OVE ISOs with embedded SSH keys and pull secrets via the existing discovery.ign endpoint
- Generate OVE-specific ignition using `openshift-install agent create unconfigured-ignition --interactive`
- Support a new `disconnected-interactive-iso` image type in InfraEnv resources

## Proposal

The proposal extends the assisted-service to generate OVE-specific ignition when serving discovery.ign for InfraEnvs with the `disconnected-interactive-iso` image type. This ignition is generated using the openshift-install binary's unconfigured-ignition command, which creates the proper structure expected by OVE ISOs.

### User Stories

#### Story 1
As a user deploying OpenShift in a disconnected environment, I want to download an OVE ISO with my SSH key and pull secret pre-configured, so that I can automate the deployment process without manual post-boot configuration.

### Implementation Details/Notes/Constraints

**OVE Ignition Generator Component:**
The `OVEIgnitionGenerator` struct encapsulates all OVE ignition generation logic:
```go
type OVEIgnitionGenerator struct {
    executer               executer.Executer
    mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder
    installerCache         installercache.InstallerCache
    versionsHandler        versions.Handler
    log                    logrus.FieldLogger
    workDir                string
}
```

**Ignition Generation Flow:**
1. Validate that InfraEnv is bound to a cluster (required for version determination)
2. Retrieve the cluster from the database to get the exact OpenShift version
3. Create temporary directory structure with required assets:
   - `cluster-manifests/infraenv.yaml` - InfraEnv manifest with user's SSH key
   - `cluster-manifests/pull-secret.yaml` - User's pull secret for registry access
   - `mirror/registries.conf` - OVE-specific registry mirror configuration
4. Fetch the appropriate openshift-install binary matching the cluster version
5. Execute `openshift-install agent create unconfigured-ignition --interactive`
6. Return the ignition content

**Key Constraints:**
- InfraEnv must be bound to a cluster to determine the OpenShift version
- The cluster's OpenShift version is used to ensure exact version match
- The openshift-install binary version must match the OVE ISO version exactly
- Registry configuration includes hardcoded OVE mirror registries:
  - `quay.io/openshift-release-dev/ocp-release` → `registry.appliance.openshift.com:5000/openshift/release-images`
  - `quay.io/openshift-release-dev/ocp-v4.0-art-dev` → `registry.appliance.openshift.com:5000/openshift/release`

**Integration Points:**
- `bareMetalInventory.V2DownloadInfraEnvFiles`: Modified to detect `disconnected-interactive-iso` type and call OVE ignition generation
- Installer cache: Reused for managing openshift-install binaries
- InfraEnv and Cluster models: Used to retrieve configuration data
- Temporary directory management: Creates isolated workspace for each generation

### Risks and Mitigations

**Risk**: Version mismatch between OVE ISO and generated ignition
- **Mitigation**: Use cluster's exact OpenShift version when fetching openshift-install binary

**Risk**: openshift-install binary not available for required version
- **Mitigation**: Leverage existing installer cache infrastructure with proper error handling

**Risk**: Temporary directory cleanup failures could leak disk space
- **Mitigation**: Implement proper cleanup with deferred functions and log warnings on cleanup errors

## Design Details

### API Changes

**InfraEnv Image Type Addition:**
Add new image type `disconnected-interactive-iso` to the InfraEnv model. This type indicates that the InfraEnv is intended for use with OVE ISOs.

**Discovery.ign Endpoint Behavior:**
The existing `/v2/infra-envs/{infra_env_id}/downloads/files?file_name=discovery.ign` endpoint behavior is modified:
- For `disconnected-interactive-iso` type: Returns OVE ignition generated via openshift-install
- For other types: Returns standard discovery ignition (unchanged)

No new endpoints are added, maintaining API compatibility.
