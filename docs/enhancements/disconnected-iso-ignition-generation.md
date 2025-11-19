---
title: disconnected-ignition-generation
authors:
  - "@ovishlit"
creation-date: 2025-09-04
last-updated: 2025-09-04
---

# Disconnected Ignition Generation

## Summary

Disconnected ISOs require manual configuration after boot, making automation hard in air-gapped environments. This enhancement document introduces a way to embed user configuration (SSH keys, pull secrets) directly into the ISO by introducing a new cluster type that serves as a configuration holder. This allows users to download customized ISOs that work with less manual intervention.

## Motivation

Disconnected ISOs are designed to deploy Openshift in disconnected environments by being completely self-contained. 
The current workflow requires two separate phases:

1. **Above-sea-level** (SaaS UI): Users download a generic disconnected ISO
2. **Below-sea-level** (Post-boot): Users manually configure essential settings through an interactive GUI

This separation creates several challenges:
- Manual intervention is required for every deployment
- Automation of disconnected installations is not possible
- Configuration errors can occur during manual entry
- The deployment process is slower and more error-prone

By allowing users to specify configuration values in the SaaS UI (above-sea-level) before downloading the ISO, we merge these two phases. Users can provide their SSH keys and pull secrets, and the resulting ISO contains these pre-configured values, reducing manual post-boot configuration while the interactive flow remains available for completing the installation process.

### Goals

- Enable users to download disconnected ISOs with pre-embedded configuration (SSH keys, pull secrets)
- Ensure temporary configuration clusters don't interfere with normal cluster operations
- Establish extensible architecture for future configuration options

### UI Integration

This feature will be integrated into the existing assisted installer UI through a "disconnected cluster" toggle. When enabled:
- Users fill in the standard cluster configuration details
- The UI calls the new `/v2/clusters/disconnected` endpoint to create a configuration cluster
- This creates a cluster with `DisconnectedCluster` kind and `unmonitored` status
- The UI then creates an InfraEnv with `disconnected-iso` image type, associating it with this cluster
- When the ISO is downloaded, it will contain the customized ignition with embedded configuration
- The downloaded ISO can then be used to install actual clusters in disconnected environments

The `DisconnectedCluster` kind ensures these configuration containers are excluded from monitoring and never enter the normal cluster lifecycle. They exist solely to hold configuration for ISO generation.

## Proposal

Introduce a new cluster type (`DisconnectedCluster`) that serves as a configuration container for ISO generation. These clusters are created via `/v2/clusters/disconnected`, start directly in `unmonitored` status bypassing the state machine, and are excluded from monitoring. When an InfraEnv with `disconnected-iso` image type is associated with such a cluster, the discovery.ign endpoint returns customized ignition generated using openshift-install's unconfigured-ignition command.

### User Stories

#### Story 1
As a user deploying Openshift in a disconnected environment, I want to download a disconnected ISO with my SSH key and pull secret pre-configured, so that I can automate the deployment process with less manual post-boot configuration.

## Implementation Details/Notes/Constraints

**Ignition Generator Component:**
The `OVEIgnitionGenerator` struct encapsulates the ignition generation logic. The implementation currently uses "OVE" terminology because it's specifically designed for OVE ISOs - the only disconnected ISO type currently supported. When additional disconnected ISO types are added, this implementation can be extended or replaced as needed:
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
When `discovery.ign` is requested for an InfraEnv with `disconnected-iso` image type:
1. Validate that InfraEnv is bound to a cluster (required for version determination)
2. Retrieve the cluster from the database to get the exact Openshift version
3. Create temporary directory structure with required assets:
   - `cluster-manifests/infraenv.yaml` - InfraEnv manifest with user's SSH key
   - `cluster-manifests/pull-secret.yaml` - User's pull secret for registry access
   - `mirror/registries.conf` - OVE-specific registry mirror configuration
4. Fetch the appropriate openshift-install binary matching the cluster version
5. Execute `openshift-install agent create unconfigured-ignition`
6. Return the ignition content

**Key Constraints:**
- InfraEnv must be bound to a cluster to determine the Openshift version
- The openshift-install binary version must match the disconnected ISO version exactly
- Registry configuration includes hardcoded OVE mirror registries:
  - `quay.io/openshift-release-dev/ocp-release` → `registry.appliance.openshift.com:5000/openshift/release-images`
  - `quay.io/openshift-release-dev/ocp-v4.0-art-dev` → `registry.appliance.openshift.com:5000/openshift/release`

**Integration Points:**
- `bareMetalInventory.V2RegisterDisconnectedCluster`: New handler for creating disconnected clusters
- `bareMetalInventory.registerClusterInternalWithKind`: Modified to support different cluster kinds and initial statuses
- `bareMetalInventory.V2DownloadInfraEnvFiles`: Modified to detect `disconnected-iso` type and call OVE ignition generation
- `cluster.RegisterCluster`: Modified to accept status parameter for direct status assignment
- Temporary directory management: Creates isolated workspace for each generation

## Design Details

### API Changes

**New Endpoint:**
- **POST** `/v2/clusters/disconnected` - Creates a cluster with `DisconnectedCluster` kind and `unmonitored` status
  - Accepts a new set of parameters specific to disconnected clusters (`disconnected-cluster-create-params`)
  - Returns cluster object with kind set to `DisconnectedCluster`
  - Cluster is created directly in `unmonitored` status without state machine transitions

**New Parameter Type:**
- `disconnected-cluster-create-params`: A dedicated parameter set for disconnected clusters
  - Contains only `name` and `openshift_version` fields
  - SSH keys and pull secrets are provided through the InfraEnv when creating the ISO
  - May be expanded in the future if additional configuration is needed

**Cluster Model Changes:**
- Add `DisconnectedCluster` to the `kind` enum (alongside `Cluster` and `AddHostsCluster`)
- Add `unmonitored` as a new cluster status

**InfraEnv Image Type Addition:**
- Add new image type `disconnected-iso` to the InfraEnv model
- This type indicates that the InfraEnv is intended for use with disconnected ISOs

**Discovery.ign Endpoint Behavior:**
The existing `/v2/infra-envs/{infra_env_id}/downloads/files?file_name=discovery.ign` endpoint behavior is modified:
- For `disconnected-iso` type: Returns the OVE ignition with the above sea level values embedded in it.
- For other types: unchanged

**Host Registration Behavior:**
- `POST /v2/infra-envs/{infra_env_id}/hosts` returns 400 when the InfraEnv has `image_type=disconnected-iso`.
- Error reason: "Cannot register a host to an InfraEnv with disconnected-iso type".

### Why Use Cluster as Configuration Holder

While SSH keys and pull secrets are provided through the InfraEnv, we use a cluster entity as the configuration holder for several important reasons:

**UI Integration:**
- The existing UI already has support for displaying clusters
- InfraEnvs are not exposed in the UI

**Future Extensibility:**
- Future enhancements may require embedding additional configuration such as network settings, operators, and other cluster-specific settings
- Some of these settings (like operators) exist only on clusters, not on InfraEnvs
- Duplicating all cluster fields to InfraEnv would create maintenance burden and confusion

### Cluster Lifecycle Management

**Disconnected Cluster Creation:**
The new `/v2/clusters/disconnected` endpoint creates clusters with special characteristics:
- **Kind**: Set to `DisconnectedCluster` to distinguish from regular clusters
- **Status**: Created directly in `unmonitored` status, bypassing the state machine
- **Status Info**: Set to "Cluster created for offline installation"
- **Purpose**: Serves purely as a configuration holder for ISO generation

**Direct Status Assignment:**
Unlike regular clusters that start in `insufficient` status and transition through the state machine, disconnected clusters:
- Are created with their final status (`unmonitored`) immediately
- Skip all state machine transitions and validations
- Never participate in the normal cluster lifecycle
- The status is determined in `registerClusterInternalWithKind` based on the cluster kind:
  - `DisconnectedCluster` → `unmonitored`
  - `AddHostsCluster` → `adding-hosts`
  - Regular `Cluster` → `insufficient`

**Monitoring Exclusion:**
Clusters with `unmonitored` status are excluded from monitoring:
- The monitoring query generator filters out `unmonitored` status at the database level
- These clusters never appear in monitoring cycles
- No background processes attempt to transition or validate these clusters
- Same exclusion mechanism used for `installed` clusters

**Implementation Flow:**
1. **UI calls** `/v2/clusters/disconnected` with cluster configuration
2. **Handler invokes** `RegisterDisconnectedClusterInternal` which calls `registerClusterInternalWithKind` with `DisconnectedCluster` kind
3. **Backend creates** cluster with appropriate status based on kind
4. **UI creates** InfraEnv with `disconnected-iso` image type, associating it with the cluster
5. **ISO Download**: When discovery.ign is requested, OVE-specific ignition is generated
6. **Cleanup**: Cluster can be deleted normally when no longer needed

This approach avoids state machine complexity while maintaining clear separation between configuration containers and actual clusters.

