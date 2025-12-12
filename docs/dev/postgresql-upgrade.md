# PostgreSQL Major Version Upgrade

This document describes how assisted-service handles PostgreSQL major version upgrades in the kube-api (MCE/ACM) deployment mode.

## Overview

PostgreSQL major version upgrades require data migration because the on-disk format changes between versions. The assisted-service uses an InitContainer-based approach that runs `pg_upgrade` with hardlink mode before the main postgres sidecar starts.

## How It Works

### InitContainer: postgres-upgrade

When the assisted-service pod starts, the `postgres-upgrade` InitContainer runs first:

1. **Check Current Version**: Reads `/var/lib/pgsql/data/userdata/PG_VERSION`
2. **Compare with Target**: If current version matches target, exit (no upgrade needed)
3. **Version Validation**: Only supports upgrading from the expected previous version (e.g., 12→13)
4. **Run Upgrade**: Sets `POSTGRESQL_UPGRADE=hardlink` and runs the container's `run-postgresql` script
5. **Wait for Completion**: Polls `pg_isready` until postgres is ready, then shuts it down
6. **Main Container Starts**: The postgres sidecar starts normally with the upgraded data

### Hardlink Mode

The `--link` flag tells `pg_upgrade` to create hardlinks instead of copying files:

- **Fast**: Completes in seconds regardless of database size
- **No Extra Storage**: Hardlinks share the same disk blocks as original files
- **Atomic**: Hardlink creation is an atomic filesystem operation

### Auto-Recovery

The InitContainer tracks upgrade attempts in `/var/lib/pgsql/data/.upgrade-attempts`. After 3 consecutive failures, it performs auto-recovery:

```bash
rm -rf /var/lib/pgsql/data/*
```

This is safe because assisted-service controllers reconcile state from Kubernetes CRs:
- **InfraEnv** CRs → `infra_envs` table
- **Agent** CRs → `hosts` table
- **AgentClusterInstall** CRs → `clusters` table

## What Gets Lost on Auto-Recovery

If auto-recovery triggers (data wipe after 3 failed upgrade attempts):

| Data | Source | Recovery |
|------|--------|----------|
| Clusters | AgentClusterInstall CR | Reconciled from etcd |
| Hosts | Agent CR | Reconciled from etcd |
| InfraEnvs | InfraEnv CR | Reconciled from etcd |
| **Events** | PostgreSQL only | **Lost** |
| **Logs metadata** | PostgreSQL only | **Lost** |

## Upgrade Path

PostgreSQL container images from [sclorg](https://github.com/sclorg/postgresql-container) include binaries for the previous major version, enabling single-step upgrades. Each image only supports upgrading from one specific previous version (`POSTGRESQL_PREV_VERSION`).

### Available Images and Supported Upgrades

| Image | PG Version | Upgrades From | Base OS |
|-------|------------|---------------|---------|
| postgresql-12-c8s | 12 | 10 | RHEL 8 |
| postgresql-13-c8s | 13 | 12 | RHEL 8 |
| postgresql-13-c9s | 13 | 12 | RHEL 9 |
| postgresql-15-c9s | 15 | 13 | RHEL 9 |
| postgresql-16-c9s | 16 | 15 | RHEL 9 |
| postgresql-17-c9s | 17 | 16 | RHEL 9 |

Note: `pg_upgrade` doesn't care about the base OS - only PostgreSQL binary compatibility. So `postgresql-13-c9s` can upgrade data created by `postgresql-12-c8s`.

### Recommended Upgrade Path

One change per release for safety:

1. **PG12/c8s → PG13/c8s** - upgrade PostgreSQL only
2. **PG13/c8s → PG13/c9s** - change base OS only (no pg_upgrade needed)
3. **PG13/c9s → PG15/c9s** - upgrade PostgreSQL only
4. Continue as needed...

## How to Upgrade PostgreSQL Version

To upgrade to a new PostgreSQL version, modify `internal/controller/controllers/images.go`:

```go
const (
    PostgresTargetVersion = "15"  // New target version
    PostgresPrevVersion   = "13"  // Previous version (must match POSTGRESQL_PREV_VERSION in image)
    PostgresImageDefault  = "quay.io/sclorg/postgresql-15-c9s:latest"
)
```

The InitContainer script automatically handles the rest.

## Deployment Strategy

The assisted-service deployment uses `Recreate` strategy (not `RollingUpdate`):

```go
deploymentStrategy := appsv1.DeploymentStrategy{
    Type: appsv1.RecreateDeploymentStrategyType,
}
```

This ensures the old pod releases the PVC before the new pod starts, preventing deadlocks.

## Testing

See the test in `agentserviceconfig_controller_test.go`:

```go
It("should include postgres-upgrade init container", func() {
    // Verifies InitContainer is present with correct name and image
})
```

For manual testing, see the test plan in the JIRA ticket.
