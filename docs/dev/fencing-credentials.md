# Fencing Credentials in the Agent-Based Installer (ABI)

This document describes how fencing credentials (BMC credentials for node fencing)
flow through the Agent-Based Installer, from user-provided YAML files on the ISO
to the final `install-config.yaml` consumed by the OpenShift installer.

For the high-level TNF enhancement proposal, see
[docs/enhancements/tnf-clusters.md](../enhancements/tnf-clusters.md).

## Overview

Two-Node with Fencing (TNF) clusters require each control-plane node to have BMC
(Baseboard Management Controller) credentials so that pacemaker/corosync can fence
(power-cycle) unresponsive nodes. These credentials must reach the installer's
`install-config.yaml` under `controlPlane.fencing.credentials`.

Fencing credentials can identify their target host in two ways:

| Key type | Identification | Placed by installer in |
|----------|----------------|------------------------|
| **Hostname** | `hostname` field in YAML | Global `fencing-credentials.yaml` |
| **MAC address** | `macaddress` field in YAML | Per-host directory `host-X/fencing-credentials.yaml` |

The MAC-based approach is useful when hostnames are not yet known at ISO creation
time, which is common in bare-metal provisioning workflows.

## File Layout on the ISO

The OpenShift installer (see [openshift/installer#10684](https://github.com/openshift/installer/pull/10684))
writes host configuration files to `/etc/assisted/hostconfig/` on the discovery ISO.

### Hostname-keyed credentials

```
/etc/assisted/hostconfig/
├── fencing-credentials.yaml          # global file, hostname-keyed entries
└── ... (other global config)
```

```yaml
# fencing-credentials.yaml
- hostname: "node1.example.com"
  address: "https://bmc1.example.com"
  username: "admin"
  password: "secret"
  certificateVerification: "Enabled"
- hostname: "node2.example.com"
  address: "https://bmc2.example.com"
  username: "admin"
  password: "secret"
```

### MAC-keyed credentials

```
/etc/assisted/hostconfig/
├── host-0/
│   ├── mac_addresses                 # one MAC per line
│   ├── fencing-credentials.yaml      # single-entry, MAC-keyed
│   └── ... (role, root-device-hints)
├── host-1/
│   ├── mac_addresses
│   ├── fencing-credentials.yaml
│   └── ...
└── ... (no global fencing-credentials.yaml)
```

```yaml
# host-0/fencing-credentials.yaml
- macaddress: "aa:bb:cc:dd:ee:ff"
  address: "https://bmc1.example.com"
  username: "admin"
  password: "secret"
```

```
# host-0/mac_addresses
AA:BB:CC:DD:EE:FF
```

A single cluster can use both keying methods simultaneously, though in practice
users typically choose one or the other.

## Data Flow

```
┌─────────────────────────────────────────────────────────────┐
│ ISO filesystem                                              │
│ /etc/assisted/hostconfig/                                   │
│   ├── fencing-credentials.yaml  (hostname-keyed)            │
│   ├── host-0/fencing-credentials.yaml  (MAC-keyed)          │
│   └── host-1/fencing-credentials.yaml  (MAC-keyed)          │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ LoadHostConfigs()                                           │
│ cmd/agentbasedinstaller/host_config.go                      │
│                                                             │
│ 1. Reads per-host dirs → MAC-based hostConfig entries       │
│    (mac_addresses normalized to lowercase)                  │
│ 2. Reads global fencing-credentials.yaml → hostname-based   │
│    hostConfig entries                                       │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ findHostConfigs()                                           │
│                                                             │
│ For each discovered host:                                   │
│   findByMAC()      → match NIC MACs (case-insensitive)      │
│   findByHostname() → match inventory hostname               │
│                                                             │
│ A host can match BOTH a MAC config and a hostname config    │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ applyFencingCredentials()                                   │
│                                                             │
│ Calls config.FencingCredentials() to resolve the correct    │
│ credential from the YAML map. Sets it on the host via       │
│ V2UpdateHost API.                                           │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ V2UpdateHost → updateHostFencing()                          │
│ internal/bminventory/inventory.go                           │
│                                                             │
│ Validates:                                                  │
│   1. TNF_CLUSTERS_SUPPORT env var is true                   │
│   2. Cluster OCP version ≥ 4.20                             │
│   3. Platform is baremetal or none                           │
│                                                             │
│ Serializes FencingCredentialsParams to JSON → stores on     │
│ host.FencingCredentials (text column in DB)                  │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ handleFencing()                                             │
│ internal/installcfg/builder/builder.go                      │
│                                                             │
│ At install-config generation time:                          │
│   1. Detects TNF topology (2 masters, both with fencing     │
│      credentials, no arbiters)                              │
│   2. Deserializes each master's JSON credentials            │
│   3. Builds installcfg.FencingCredential entries            │
│      - Uses MacAddress if present, else Hostname            │
│   4. Sets controlPlane.fencing.credentials                  │
│   5. Sets TechPreviewNoUpgrade for OCP < 4.22              │
└─────────────────────────────────────────────────────────────┘
```

## Key Implementation Details

### MAC Address Case Normalization

MAC addresses can appear in different cases depending on the source:
- `mac_addresses` files may contain uppercase (`AA:BB:CC:DD:EE:FF`)
- Inventory NICs may report lowercase (`aa:bb:cc:dd:ee:ff`)
- YAML files may use either case

To ensure reliable matching, the code normalizes MAC addresses to **lowercase**
at every entry point:

| Location | Normalization |
|----------|---------------|
| `LoadHostConfigs()` reading `mac_addresses` | `strings.ToLower(strings.TrimSpace(l))` |
| `loadFencingCredentials()` map key | `strings.ToLower(cred.MACAddress)` |
| `loadFencingCredentials()` params value | `strings.ToLower(cred.MACAddress)` stored in `params.MacAddress` |
| `findByMAC()` NIC comparison | `strings.EqualFold(nic.MacAddress, mac)` |

This mirrors the installer's behavior in `findHostDirForMAC`, which also uses
`strings.ToLower`.

### Host Config Resolution

`findHostConfigs()` runs two independent lookups for each discovered host:

1. **`findByMAC()`** — iterates configs that have `macAddresses`, then iterates
   the host's inventory NICs. Returns the config if any NIC MAC matches
   (case-insensitive).

2. **`findByHostname()`** — iterates configs that have a non-empty `hostname`,
   comparing against the host's `inventory.Hostname`. Logs available credential
   hostnames on mismatch to help debug FQDN vs short-name issues.

A host can match multiple configs (e.g., a MAC-based config carrying role/disk
hints and a hostname-based config carrying fencing credentials). All matching
configs are applied.

### Credential Lookup in FencingCredentials()

The `FencingCredentials()` method on `hostConfig` reads the
`fencing-credentials.yaml` from the config's own directory (`hc.configDir`):

- **Hostname-based config**: direct map lookup by `hc.hostname`
- **MAC-based config**: iterates `hc.macAddresses`, looking for a key match in
  the credentials map (both are lowercase-normalized)

### Feature Gating

TNF support is gated at multiple levels:

| Gate | Location | Default |
|------|----------|---------|
| `TNF_CLUSTERS_SUPPORT` env var | `inventory.go` L128 | `false` |
| OCP version ≥ 4.20 | `ValidateClusterSupportsFencingCredentials()` | — |
| Platform baremetal or none | `ValidateClusterSupportsFencingCredentials()` | — |
| Host pre-installation state | `host.UpdateFencing()` | — |
| `TechPreviewNoUpgrade` feature set | `handleFencing()` for OCP < 4.22 | — |

TNF is disabled in SaaS. The `TNF_CLUSTERS_SUPPORT` env var must be explicitly
set to `true`.

### TNF Topology Detection

A cluster is detected as TNF (`IsClusterTopologyTwoNodesWithFencing()`) when all
of these are true:

- `cluster.ControlPlaneCount == 2`
- No arbiter hosts present
- Exactly 2 master-role hosts
- Both masters have non-empty `FencingCredentials`

This distinguishes TNF from Two-Node Arbiter (TNA) topology, which has 2 masters
plus an arbiter node.

## Data Model

### Swagger API (`swagger.yaml`)

```yaml
fencing-credentials-params:
  type: object
  required: [address, username, password]
  properties:
    address:      { type: string }  # BMC URL (e.g., https://bmc1.example.com)
    username:     { type: string }
    password:     { type: string }
    certificate_verification:
      type: string
      enum: [Enabled, Disabled]
      default: Enabled
    mac_address:  { type: string, x-nullable: true }  # MAC-based identification
```

On the `host` model, `fencing_credentials` is stored as a JSON-serialized string
(`gorm:"type:text"`), not a structured object.

On `host-update-params`, `fencing_credentials` is a `$ref` to
`fencing-credentials-params` (structured object).

### Install-Config Output (`internal/installcfg/installcfg.go`)

```go
type FencingCredential struct {
    Hostname                string                   `json:"hostname,omitempty"`
    MacAddress              string                   `json:"macAddress,omitempty"`
    Address                 string                   `json:"address"`
    Username                string                   `json:"username"`
    Password                string                   `json:"password"`
    CertificateVerification *CertificateVerification `json:"certificateVerification,omitempty"`
}
```

Exactly one of `Hostname` or `MacAddress` is populated per credential, depending
on how the user originally identified the host.

### YAML Input Format (`fencing-credentials.yaml`)

```go
type fencingCredential struct {
    Hostname                string `yaml:"hostname"`
    MACAddress              string `yaml:"macaddress"`
    Address                 string `yaml:"address"`
    Username                string `yaml:"username"`
    Password                string `yaml:"password"`
    CertificateVerification string `yaml:"certificateVerification"`
}
```

## Kube-API Mode

In Kube-API mode, fencing credentials are handled differently:

- The `Agent` CRD has a `FencingCredentialsSecretRef` field referencing a
  Kubernetes Secret containing BMC credentials.
- For ZTP (Zero Touch Provisioning), a BMH annotation specifies the Agent's
  `FencingCredentialsSecretRef`.
- If the `AgentClusterInstall` specifies 2 CP nodes and 0 arbiter nodes, the
  cluster is treated as TNF and the Agent controller updates each host's
  fencing credentials from the referenced Secret.

## Security Considerations

BMC credentials are stored as **plaintext** in:
1. The assisted-service database (`host.fencing_credentials` text column)
2. The cluster's `install-config.yaml` and `bootstrap.ign` (saved to object
   storage)
3. Temporarily on the pod's filesystem during ignition generation

This was an explicit design decision for non-SaaS installations, documented in
the [OpenShift TNF enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/two-node-fencing/tnf.md#assisted-installer-family-changes).
TNF is disabled in SaaS via the `TNF_CLUSTERS_SUPPORT` env var.

## Related Files

| File | Purpose |
|------|---------|
| `cmd/agentbasedinstaller/host_config.go` | ABI host config loading, MAC/hostname matching, credential application |
| `cmd/agentbasedinstaller/host_config_test.go` | Unit tests for the above |
| `internal/installcfg/builder/builder.go` | Install-config generation (`handleFencing`) |
| `internal/installcfg/installcfg.go` | Install-config structs (`FencingCredential`, `Fencing`) |
| `internal/bminventory/inventory.go` | API handler (`updateHostFencing`) |
| `internal/host/host.go` | Host state validation (`UpdateFencing`) |
| `internal/common/common.go` | TNF detection (`IsClusterTopologyTwoNodesWithFencing`) |
| `swagger.yaml` | API spec (`fencing-credentials-params`, `host-update-params`) |
| `models/fencing_credentials_params.go` | Generated model struct |
