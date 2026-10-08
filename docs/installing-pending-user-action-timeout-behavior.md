# Timeout Behavior for Hosts in "installing-pending-user-action" State

## Overview

This document explains how hosts reach the "installing-pending-user-action" state and describes the timeout behavior for both hosts and clusters when hosts are in this state.

## How Hosts Reach "installing-pending-user-action"

Hosts can transition to the `installing-pending-user-action` state in two scenarios:

### 1. Wrong Boot Order Detection (Most Common)

**Trigger:** A host boots from the discovery ISO after it should have booted from the installed operating system.

**When it happens:**
- The host has already started installation and reached the rebooting stage
- After the reboot, the host boots from the discovery ISO instead of the installed OS disk
- The discovery ISO immediately tries to register with the service
- The service detects this is wrong and moves the host to `installing-pending-user-action`

**State machine transitions:**
- **Day-1 hosts:** From `installing-in-progress` or `installing` → `installing-pending-user-action`
  - Condition: Host is in rebooting stage AND registers (indicating wrong boot device)
  - Code: `internal/host/statemachine.go:189-201`

- **Day-2 hosts:** From `installing-in-progress` or `added-to-existing-cluster` → `installing-pending-user-action`
  - Condition: Host is in done stage AND registers
  - Code: `internal/host/statemachine.go:189-201`

**User-facing message:**
```
Expected the host to boot from disk, but it booted the installation image - please 
reboot and fix boot order to boot from disk [Model] [Serial] (disk-name, disk-id)
```

Where `[Model]` and `[Serial]` are the installation disk's model and serial number (if available), and `disk-name` and `disk-id` identify the specific disk.

**Root causes:**
- BIOS/UEFI boot order configured incorrectly
- Installation disk not set as first boot device
- Virtual media still mounted with higher priority than installation disk
- User manually selected discovery ISO during boot

### 2. Reboot Timeout

**Trigger:** A host takes too long to reboot and doesn't make progress in the rebooting stage.

**When it happens:**
- Host is in `installing-in-progress` state
- Host is in the `rebooting` stage
- The stage timeout expires before the host completes the reboot

**Timeouts:**
- **Multi-node clusters:** 40 minutes (default)
  - Configurable via `HOST_STAGE_REBOOTING_TIMEOUT` environment variable
- **Single Node OpenShift (SNO):** 80 minutes (hardcoded)
  - Extended timeout due to additional bootstrap responsibilities

**State machine transition:**
- From: `installing-in-progress` (while in rebooting stage)
- To: `installing-pending-user-action`
- Condition: Stage timeout expired AND host is in rebooting stage
- Code: `internal/host/statemachine.go:921-933`

**User-facing message:**
```
Host timed out when pulling the configuration files. Verify in the host console 
that the host boots from the OpenShift installation disk (disk-name, disk-id) and 
has network access to the cluster API. The installation will resume after the host 
successfully boots and can access the cluster API
```

Where `disk-name` and `disk-id` identify the installation disk (e.g., `(sda, /dev/disk/by-id/...)`)

**Root causes:**
- Host hardware taking unusually long to reboot
- Host stuck in BIOS/firmware
- Network connectivity issues preventing host from reporting progress
- Actual wrong boot order (boots to ISO but doesn't register immediately)

### Key Differences Between the Two Scenarios

| Aspect | Wrong Boot Order Detection | Reboot Timeout |
|--------|----------------------------|----------------|
| **Trigger** | Host actively registers while in rebooting stage | Rebooting stage timeout expires |
| **Detection** | Immediate (on registration) | After 40min (multi-node) or 80min (SNO) |
| **Message keyword** | "booted the installation image" | "timed out when pulling configuration" |
| **Certainty** | Definitely wrong boot order | Could be timeout OR wrong boot order |
| **Code path** | `PostRegisterDuringReboot` | `PostRefreshHost` with `statusRebootTimeout` |
| **Event handler** | `TransitionTypeRegisterHost` | `TransitionTypeRefresh` |

**Important:** Both scenarios result in the same state (`installing-pending-user-action`), but the messages differ to help users understand what happened. The "installation image" message is more specific because the service detected the host actually booted from the discovery ISO, while the timeout message is more ambiguous since the service doesn't know for certain what happened.

## Host-Level Timeout Behavior

### While in "installing-pending-user-action"

**Key point:** Hosts in `installing-pending-user-action` state will now timeout after a configurable duration.

**Timeout Configuration:**
- **Default:** 60 minutes
- **Configurable via:** `HOST_INSTALLING_PENDING_USER_ACTION_TIMEOUT` environment variable
- **Measured from:** When the host entered `installing-pending-user-action` state (using `status_updated_at` field)

**Behavior:**
- The host remains in `installing-pending-user-action` waiting for user intervention
- After the timeout expires, the host transitions to `error` state
- The host can recover before timeout by booting from the correct disk

**Recovery path (before timeout):**
1. User fixes the boot order or manually boots from installation disk
2. Host boots into the installed OS
3. Host begins pulling configuration and continues installation
4. Host reports progress via its stage
5. Host transitions back to `installing-in-progress` or progresses to `installed`

**Timeout behavior:**
- After timeout expires: host transitions to `error` state **only if cluster can succeed without it**
- Cluster viability check ensures critical hosts (e.g., masters in 3-node cluster) don't timeout
- If cluster needs this host to succeed, host stays in `installing-pending-user-action` (cluster 24h timeout applies)
- Error message: "Host failed to boot from the installation disk within the expected time. The host had wrong boot order and did not recover. To include this host, reset the cluster and fix the boot order, or continue installation with remaining hosts"
- Code: `internal/host/statemachine.go` (transition with cluster viability check)

**State machine behavior:**
- Refresh transitions keep the host in `installing-pending-user-action` until timeout expires
- After timeout: transition to `error` via `HasPendingUserActionTimedOut` condition
- HostProgress transitions can move the host out of this state when progress is detected (before timeout)

## Cluster-Level Timeout Behavior

### Transition INTO "installing-pending-user-action"

**Trigger:** At least one host in the cluster enters `installing-pending-user-action`

**Conditions:**
- Cluster is in `installing` or `finalizing` state
- At least one host has status `installing-pending-user-action`
- The cluster still has enough hosts in installing/installed states to potentially succeed

**State machine transition:**
- From: `installing` or `finalizing`
- To: `installing-pending-user-action`
- Condition: `IsInstallingPendingUserAction` returns true (checks if any host has this status)
- Code: `internal/cluster/statemachine.go:457-471`

**Status message:** "Cluster has hosts pending user action"

### Timeout While Cluster is in "installing-pending-user-action"

**Installation Timeout:**
- **Default:** 24 hours
- **Configurable via:** `INSTALLATION_TIMEOUT` environment variable
- **Start time:** When installation began (not when cluster entered pending state)

**When timeout expires:**
- Cluster transitions to `error` state
- Error message: "cluster installation timed out while pending user action (a manual booting from installation disk)"
- Code: `internal/cluster/statemachine.go:320-332`

**Important:** The timeout is measured from the original installation start time, NOT from when the cluster entered `installing-pending-user-action`. This means:
- If a cluster enters pending state after 23 hours of installation, it only has 1 hour to recover
- The timeout is checking overall installation duration, not time spent in pending state

### Other Timeout Scenarios

While in `installing-pending-user-action`, the cluster can also transition to error if:

1. **Not enough hosts to proceed:**
   - Condition: Cluster no longer has enough hosts in installing/finalizing states
   - Result: Move to `error` with message "cluster has hosts in error"
   - Code: `internal/cluster/statemachine.go:301-318`

### Recovery from "installing-pending-user-action"

The cluster can transition OUT of `installing-pending-user-action` when hosts recover:

**Back to Installing:**
- Condition: No hosts are pending user action AND enough hosts are still installing (but not yet finalizing)
- Transition: `installing-pending-user-action` → `installing`
- Message: "Installation in progress"
- Code: `internal/cluster/statemachine.go:421-437`

**To Finalizing:**
- Condition: No hosts are pending user action AND enough masters and workers are installed
- Transition: `installing-pending-user-action` → `finalizing`
- Message: "Finalizing cluster installation"
- Code: `internal/cluster/statemachine.go:439-454`

**Stay in Pending (no-op):**
- Condition: Still have hosts pending user action, but enough hosts are installing or finalizing
- Transition: `installing-pending-user-action` → `installing-pending-user-action`
- Code: `internal/cluster/statemachine.go:404-419`

### Cluster Success with Timed-Out Hosts

**New Behavior:** When hosts timeout from `installing-pending-user-action`, the system intelligently decides whether to move them to `error` based on cluster viability.

**How It Works:**

1. **Host Timeout with Viability Check:**
   - Host in `installing-pending-user-action` exceeds `HOST_INSTALLING_PENDING_USER_ACTION_TIMEOUT` (default: 60 minutes)
   - System checks: "Would the cluster still succeed if this host goes to error?"
   - **If YES** (cluster has enough remaining hosts): Host transitions to `error` state
   - **If NO** (cluster needs this host): Host stays in `installing-pending-user-action`, giving user more time to fix
   - This prevents timing out critical hosts (e.g., masters in 3-node cluster)

2. **Cluster Re-evaluation:**
   - Cluster checks if any hosts are still in `installing-pending-user-action`
   - If no hosts pending: cluster exits `installing-pending-user-action` state
   - Cluster checks if it has enough hosts to succeed (via `IsInstalling` or `IsFinalizing`)

3. **Sufficient Hosts Check:**
   - Hosts in `error` state are NOT counted toward minimum requirements
   - **For Workers:** Cluster needs minimum 2 workers in good states (installing, installed, etc.)
     - Example: 5-worker cluster with 2 workers in error → succeeds (3 healthy ≥ 2 minimum)
   - **For Masters:** Cluster needs ALL expected masters in good states
     - Example: 3-master cluster with 1 master in error → FAILS (2 healthy < 3 required)

4. **Cluster Progression:**
   - If sufficient hosts: cluster moves to `installing` or `finalizing` → `installed`
   - If insufficient hosts: cluster moves to `error`

**Practical Examples:**

**Scenario 1: Worker failure - Cluster succeeds**
- 3-master + 5-worker cluster
- 2 workers stuck in `installing-pending-user-action`
- After 60 minutes: 2 workers → `error`
- Cluster has 3 masters + 3 workers (healthy)
- Result: Cluster succeeds (3 workers ≥ 2 minimum)

**Scenario 2: Master failure - Smart timeout**
- 3-master cluster (requires all 3 masters)
- 1 master stuck in `installing-pending-user-action`
- After 60 minutes: **Master stays in `installing-pending-user-action`** (viability check prevents timeout)
- Reason: Cluster needs all 3 masters; timing out would immediately fail cluster
- User has until 24-hour cluster timeout to fix boot order
- Result: Better UX - user gets more time to recover critical host

**Scenario 3: Extra workers, some fail**
- 3-master + 3-worker cluster
- 1 worker stuck in `installing-pending-user-action`
- After 60 minutes: 1 worker → `error` (cluster still has 2 workers ≥ minimum)
- Cluster has 3 masters + 2 workers (healthy)
- Result: Cluster succeeds (2 workers ≥ 2 minimum)

**Scenario 4: Insufficient workers - Smart timeout**
- 3-master + 2-worker cluster (minimum workers)
- 1 worker stuck in `installing-pending-user-action`
- After 60 minutes: **Worker stays in `installing-pending-user-action`** (viability check prevents timeout)
- Reason: Only 2 workers total; timing out would leave only 1 worker (< 2 minimum)
- Result: User gets more time to recover the critical worker

**Code References:**
- Host timeout transition: `internal/host/statemachine.go` (HasPendingUserActionTimedOut condition)
- Cluster validation: `internal/cluster/transition.go:517-554` (enoughMastersAndWorkers function)
- Host counting: `internal/cluster/common.go:192-206` (HostsInStatus function)

## Summary Table

| Aspect | Behavior | Timeout Value | Configurable |
|--------|----------|---------------|--------------|
| **Host entering state** | Wrong boot order OR reboot timeout | N/A (event-driven) | No |
| **Host reboot timeout (multi-node)** | Triggers transition to pending state | 40 minutes | Yes (`HOST_STAGE_REBOOTING_TIMEOUT`) |
| **Host reboot timeout (SNO)** | Triggers transition to pending state | 80 minutes | No (hardcoded) |
| **Host while in pending state** | Timeout to error if cluster viable | 60 minutes | Yes (`HOST_INSTALLING_PENDING_USER_ACTION_TIMEOUT`) |
| **Host after timeout** | Transitions to error (only if cluster can succeed) | N/A (triggered by viability check) | N/A |
| **Cluster timeout** | Entire installation must complete | 24 hours (from install start) | Yes (`INSTALLATION_TIMEOUT`) |
| **Cluster recovery** | Returns to installing/finalizing when hosts recover or timeout | N/A | N/A |
| **Cluster success with failures** | Workers can fail if minimum met; masters cannot | Workers: ≥2, Masters: all required | N/A |

## Critical Implementation Details

### Host Stage Timeouts
- Defined in: `internal/host/config.go`
- Default values in `hostStageTimeoutDefaults` map
- Rebooting stage: 40 minutes (except SNO which is 80 minutes)
- All stage timeouts are configurable via `HOST_STAGE_<STAGE_NAME>_TIMEOUT` environment variables

### Cluster Installation Timeout
- Defined in: `internal/cluster/cluster.go`
- Field: `InstallationTimeout` in `Config` struct
- Default: 24 hours
- Environment variable: `INSTALLATION_TIMEOUT`
- **Critical:** Timeout is measured from `cluster.InstallStartedAt`, not from when cluster enters pending state

### Wrong Boot Order Stages
The following stages are considered "wrong boot order" stages where timeout should be ignored if the cluster is in pending state:
- `waiting-for-control-plane`
- `waiting-for-controller`
- `waiting-for-bootkube`
- `rebooting`

Defined in: `internal/host/host.go:41-46` as `WrongBootOrderIgnoreTimeoutStages`

## Related Code References

### State Machines
- Host state machine: `internal/host/statemachine.go`
- Host transitions: `internal/host/transition.go`
- Cluster state machine: `internal/cluster/statemachine.go`
- Cluster transitions: `internal/cluster/transition.go`

### Status Messages
- Host status constants: `internal/host/common.go:49` (defines `statusRebootTimeout`)
- Wrong boot order message: `internal/host/transition.go` (in `PostRegisterDuringReboot` function)
  - Line ~234: "Expected the host to boot from disk, but it booted the installation image..."
- Reboot timeout message: `internal/host/common.go:49`
  - Constant: `statusRebootTimeout` 
- Message macro replacement: `internal/host/transition.go` (in `replaceMacros` function)
  - Replaces `$INSTALLATION_DISK` with actual disk information

### Configurations
- Host timeout configurations: `internal/host/config.go`
- Cluster timeout configurations: `internal/cluster/cluster.go`
- Cluster status constants: `internal/cluster/common.go`
