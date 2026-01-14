---
title: third-party-cni-support
authors:
  - "@rivkyrizel"
creation-date: 2025-12-16
last-updated: 2025-12-16
---

# Third-Party CNI Support for Assisted Installer

## Summary

Assisted Installer currently enforces OVN-Kubernetes (OVN-K) as the only supported Container Network Interface (CNI) for all cluster installations. This enhancement exposes the `network_type` field directly in the API (instead of requiring install-config overrides) for Red Hat certified third-party CNI plugins, allowing customers to install OpenShift clusters with Cisco ACI, Isovalent Cilium, or Tigera Calico without post-installation workarounds. Users will still need to provide their own CNI manifests.

The enhancement introduces CNI selection during cluster creation, with platform and version compatibility validation. Customers can select their preferred certified CNI or install in "No CNI" mode to provide their own CNI manifests. This brings Assisted Installer to feature parity with IPI/UPI installation methods and aligns with Red Hat's CNI certification program.

## Motivation

Enterprise customers often require certified third-party CNIs for regulatory compliance, existing infrastructure integration, or advanced networking capabilities. Currently, these customers must install with OVN-K and then manually replace it post-installation—an unsupported, error-prone process that increases deployment time and complexity.

OpenShift supports multiple certified CNI providers through its certification program. IPI and UPI installation methods already enable these CNIs through documented procedures. Assisted Installer should provide the same flexibility, allowing customers to benefit from simplified deployment workflows while using their required networking solution.

### Goals

- Enable installation of OpenShift clusters with third-party CNIs
- Support Red Hat certified CNI plugins as first-class options:
  - Cisco ACI (Application Centric Infrastructure)
  - Isovalent Cilium (Community and Enterprise)
  - Tigera Calico (Calico Core)
- Validate CNI compatibility with platform, OCP version, and cluster topology
- Provide clear guidance and documentation links for CNI-specific configuration
- Support both REST API and Kube-API modes (AgentClusterInstall CR)
- Maintain backward compatibility with existing OVN-K installations

### Non-Goals

- CNI-specific configuration or lifecycle management beyond installation (handled by CNI operators)
- Supporting uncertified or custom CNI implementations
- Modifying or replacing OVN-K as the default recommended CNI
- Automated Day 2 CNI migration or replacement
- Dual-CNI or CNI mixing scenarios
- Packaging or distributing third-party CNI manifests

## Proposal

### User Stories

#### Story 1

As a network administrator deploying OpenShift on bare metal with Cisco ACI networking infrastructure, I want to install clusters using Assisted Installer with Cisco ACI CNI so that I can leverage Assisted Installer's simplified deployment workflow while maintaining compliance with my organization's Cisco-based networking standards.

#### Story 2

As a platform engineer deploying OpenShift on vSphere, I want to use Isovalent Cilium instead of OVN-K so that I can utilize Cilium's eBPF-based networking, advanced observability features, and network policy capabilities required by my organization's security team.

#### Story 3

As a developer deploying SNO clusters for edge computing, I want to install OpenShift with Tigera Calico via Assisted Installer's API so that I can programmatically deploy edge nodes with the same CNI and network policies used in our data center clusters.

### Implementation Details/Notes/Constraints

#### Supported CNI Matrix

The following CNIs are supported based on Red Hat's CNI certification program (https://access.redhat.com/articles/5436171):

**Cisco ACI**
- Supported Platforms: Bare Metal, vSphere, OpenStack (OSP 16.2, 17.1)
- Supported OCP Versions: 4.10 - 4.19 (platform-dependent)
- ACI Versions: 5.2(1) - 6.1(2)
- Installer Types: UPI
- Known Constraints: OSP 16.2 with OCP 4.15/4.16 not supported for production use

**Isovalent Cilium**
- Supported Platforms: Bare Metal, vSphere, AWS, Azure, GCP, OpenStack, Nutanix
- Supported OCP Versions: 4.5 - 4.20
- Cilium Versions: 1.9 - 1.18
- Installer Types: UPI, IPI (version-dependent)
- Known Constraints: Some virtualization test results may be pending

**Tigera Calico**
- Supported Platforms: Bare Metal, vSphere, AWS, Azure, GCP, OpenStack
- Supported OCP Versions: 4.8 - 4.18
- Calico Versions: 3.20 - 3.30
- Installer Types: IPI, UPI (version-dependent)

#### API Changes

Extend the existing `network_type` field in `ClusterCreateParams` to support additional CNI options. The field will support the following values:
- `OVNKubernetes` (default for supported versions)
- `OpenShiftSDN` (for older OpenShift versions where still supported)
- `CiscoACI` (user must provide manifests)
- `Cilium` (user must provide manifests)
- `Calico` (user must provide manifests)
- `None` (user must provide manifests)

**CNI Version Handling:**
Assisted Installer will not manage specific CNI versions (e.g., Cilium 1.17 vs 1.18). The CNI version is determined by the manifests provided by the user. Assisted Installer only validates that the selected CNI type is compatible with the OCP version and platform. Users obtain version-specific manifests from CNI vendors and upload them via the custom manifests API.

Validation requirements:
- API validates CNI against support matrix (platform, OCP version, topology)
- Returns HTTP 400 with descriptive error for unsupported combinations
- All third-party CNIs (CiscoACI, Cilium, Calico, None) require custom manifests to be uploaded via the manifests API
- OpenShiftSDN availability is version-dependent (deprecated in newer OCP releases)

**Mapping to install-config:**

| API `network_type` | install-config `networking.networkType` |
|-------------------|----------------------------------------|
| `OVNKubernetes`   | `OVNKubernetes` |
| `OpenShiftSDN`    | `OpenShiftSDN` |
| `CiscoACI`        | `CiscoACI` |
| `Cilium`          | `Cilium` |
| `Calico`          | `Calico` |
| `None`            | (omitted or empty) |

#### Changes to assisted-service

**Provider Abstraction:**
Implement a CNI provider registry pattern (similar to existing platform providers) that allows each CNI to register its support matrix, validation rules, and documentation links. The registry will be consulted during cluster validation to determine CNI compatibility.

**Validation Improvements:**
Extend cluster validation logic to check CNI compatibility with the selected platform, OCP version, and cluster topology. Validation will reference a structured support matrix (stored as JSON) that maps CNI support to specific OCP versions and platforms.

Add new validation that blocks installation if a third-party CNI is selected but no custom manifests have been uploaded. The validation will check for the presence of manifests and display an error message directing users to upload CNI manifests before proceeding.

**Manifest Generation:**
Modify install-config generation to skip OVN-K-specific configuration when a third-party CNI is selected. The CNI will be configured via custom manifests provided by the user through the existing manifests API.

**Manifest Validation:**
Basic validation will be performed on uploaded manifests to ensure they are valid YAML/JSON and contain Kubernetes resource definitions. However, CNI-specific semantic validation (e.g., verifying Cilium operator configuration) is out of scope. Invalid manifests will cause installation failures during cluster bootstrapping, which is acceptable as users are responsible for providing correct CNI configurations.

**Feature Support Levels:**
Update the feature support levels API to expose CNI support per platform and OCP version, enabling the UI to dynamically show or hide CNI options based on cluster configuration.

#### Changes to assisted-installer-ui

The Networking configuration step will be redesigned to support CNI selection:

**Networking Step Layout:**
- Cluster-Managed Networking option displays OVN-Kubernetes or OpenShiftSDN based on OCP version
- User-Managed Networking option adds a CNI selection dropdown with the following options:
  - OVN-Kubernetes (default for newer versions)
  - OpenShiftSDN (available for older OCP versions)
  - Cisco ACI
  - Isovalent Cilium
  - Tigera Calico
  - None (Custom CNI)

**Validation and Warnings:**
- Real-time validation displays compatibility status:
  - ✓ Success: "Cisco ACI is supported for OCP 4.18 on vSphere"
  - ✗ Error: "Calico is not supported on AWS for OCP 4.15. Supported platforms: Bare Metal, vSphere, OpenStack"
- Warning banner displayed when third-party CNI is selected:
  - "Third-party CNIs require custom manifests to be uploaded. See CNI vendor documentation for manifest examples."

**Manifest Requirements:**
- **IMPORTANT**: Users selecting any CNI option other than OVN-Kubernetes or OpenShiftSDN (i.e., Cisco ACI, Cilium, Calico, or None) **must** upload CNI manifests via the Custom Manifests step. The installation will fail validation if third-party CNI is selected but no CNI-related manifests are provided.
- UI will display a link to CNI vendor documentation for obtaining correct manifests
- Manifest upload section will highlight required CNI operator and configuration manifests

**Documentation Links:**
Each CNI option displays a link to vendor-specific documentation for post-install configuration and manifest examples.

#### Kubernetes API Changes

Update `AgentClusterInstall` CR to support CNI selection through a new `networkType` field in the `spec.networking` section. Supported values include `OVNKubernetes`, `OpenShiftSDN`, `CiscoACI`, `Cilium`, `Calico`, and `None`.

The validation webhook will enforce CNI constraints, validating CNI selection against the support matrix. OpenShiftSDN availability will be validated based on OCP version compatibility.

CNI validation status (manifests uploaded, platform compatibility) will be reported via a `CNIValidated` condition in `status.conditions`, allowing users to see validation issues through the Kube API.

#### CNI Support Matrix Storage

The CNI support matrix will be stored as structured data in `data/cni_support_matrix.json`, containing:
- CNI names and display names
- Supported OCP versions per CNI
- Supported platforms per CNI/OCP version combination
- Version-specific constraints
- Documentation URLs for each CNI vendor

This structured storage enables easier updates without code changes and allows future dynamic fetching of support data from external services.

### Risks and Mitigations

**CNI Support Matrix Maintenance**
The CNI certification program updates regularly. The support matrix embedded in Assisted Installer may become outdated.

*Mitigation:* Implement dynamic support matrix fetching (similar to OCP release versions). Add CI validation to detect drift from published certification data. Consider maintaining matrix in a separate service for easier updates.

**Testing Coverage**
Assisted Installer CI is optimized for OVN-K. Third-party CNI testing may be insufficient without vendor participation.

*Mitigation:* Partner with CNI vendors (Cisco, Isovalent, Tigera) to establish joint testing programs. Mark feature as TechPreview initially. Require CNI vendors to maintain E2E test suites against Assisted Installer. Progress to GA after sufficient validation.

**Note:** Coordination with CNI vendors for joint testing is not guaranteed and requires explicit agreements. This partnership must be established as part of the implementation plan before GA release.

**Support Complexity**
Separating Assisted Installer issues from CNI operator issues may be challenging for support teams.

*Mitigation:* Implement clear logging boundaries. Develop diagnostic tools to validate CNI installation status. Create support runbooks for triaging CNI-related issues. Establish escalation paths to CNI vendor support teams. These diagnostic and support tools will be developed in Phase 3 of the implementation.

**Backward Compatibility**
Introducing CNI selection may affect existing installation workflows.

*Mitigation:* Default to OVN-K for all existing behavior. Treat third-party CNI as opt-in feature requiring explicit selection. Ensure clusters without `network_type` specified continue using OVN-K. Document that CNI selection is Day 1 only (no Day 2 migration).

## Design Details

### Phased Implementation

**Phase 1: "No CNI" Mode (TechPreview)**
- Introduce feature flag `ENABLE_NO_CNI_MODE` to allow cluster installation without OVN-K (will be removed in Phase 2)
- Require custom manifests
- Skip OVN-K manifest generation when enabled
- Add validation to block installation if manifests are not uploaded for third-party CNIs
- Provide documentation for installing with third-party CNIs
- Implement subsystem tests for CNI validation logic
- Manual validation with Cilium and Calico reference examples

**Phase 2: CNI Selection with Validation (GA Target)**
- Remove `ENABLE_NO_CNI_MODE` feature flag
- Extend `network_type` field with additional CNI options
- Implement CNI provider registry and support matrix validation
- Update UI with CNI dropdown and real-time compatibility checking
- Add integration and E2E tests with CNI vendor partners
- Integrate with feature support levels API
- Production-ready documentation and troubleshooting guides

**Phase 3: Enhanced Support**
- Dynamic CNI support matrix updates (fetched from a Red Hat-maintained service that aggregates CNI certification data, eliminating the need for Assisted Installer code updates when certifications change)
- Post-install CNI health validation and diagnostic tools
- Enhanced logging and troubleshooting capabilities
- CNI-specific observability metrics
- Support runbooks and escalation procedures

### UI Impact

The Networking configuration step will include:
- CNI selection dropdown under user-managed networking option
- Real-time validation feedback showing platform/version compatibility
- Warning banners for third-party CNIs requiring manifests
- Documentation links to CNI vendor installation guides
- Post-install guidance displaying CNI-specific next steps

**Manifest Upload Requirement:**
Users must be clearly informed that selecting a third-party CNI or "None" **requires** uploading CNI manifests. The UI will block installation progress if manifests are not provided for non-default CNI selections.

### Test Plan

**Unit Tests**
- CNI provider registration and support matrix validation
- API validation for `network_type` field
- Compatibility checking logic for platform/version/CNI combinations
- Manifest presence validation

**Subsystem Tests**
- OVN-K default behavior (ensure backward compatibility)
- Third-party CNI selection with manifests (mock CNI operator installation)
- Validation failures for unsupported platform/version/CNI combinations
- "No CNI" mode with custom manifests
- Validation failures when manifests are missing for third-party CNIs

**Integration Tests (Partner-Led)**
- Cisco ACI deployment on bare metal with OCP 4.18
- Isovalent Cilium deployment on vSphere with OCP 4.19
- Tigera Calico SNO deployment on bare metal with OCP 4.17
- Verify CNI operator pods start successfully
- Validate pod networking and connectivity

**E2E Strategy**
Partner with CNI vendors to establish joint CI infrastructure where vendors maintain E2E test suites against Assisted Installer nightly builds. Red Hat QE validates "No CNI" mode with example manifests from each vendor.

## Drawbacks

**Increased Complexity**
Supporting multiple CNIs increases the testing matrix, documentation scope, and potential support burden.

**Vendor Dependency**
Reliance on CNI vendors for testing, documentation, and support may introduce delays or gaps.

**User Experience Fragmentation**
Adding CNI selection introduces complexity for users who don't need third-party CNIs, potentially confusing the installation workflow.

**CNI Version Maintenance**
We will need to maintain CNI versions in the support matrix and test only the latest certified CNI version for each OCP release. This requires ongoing coordination with the CNI certification program and regular updates.

These drawbacks are mitigated by treating third-party CNI support as opt-in, maintaining OVN-K as the default, and establishing clear partnerships with CNI vendors.
