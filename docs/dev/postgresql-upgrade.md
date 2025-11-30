# PostgreSQL Major Version Upgrade

This document describes how assisted-service handles PostgreSQL major version upgrades in the kube-api (MCE/ACM) deployment mode.

## Overview

PostgreSQL major version upgrades require data migration because the on-disk format changes between versions. The assisted-service leverages the [sclorg postgresql-container](https://github.com/sclorg/postgresql-container) built-in upgrade mechanism via the `POSTGRESQL_UPGRADE` environment variable.

## The Problem

The sclorg containers support `POSTGRESQL_UPGRADE=hardlink` to trigger `pg_upgrade`, but this setting **cannot be set permanently**. The sclorg container intentionally fails when `POSTGRESQL_UPGRADE` is set but versions already match - this is a safety mechanism to prevent users from leaving it enabled.

## Our Solution: Conditional Upgrade

We use a wrapper script (`internal/controller/controllers/postgres_startup.sh`) embedded via `//go:embed` that conditionally sets `POSTGRESQL_UPGRADE=hardlink` only when a version mismatch is detected.

The script:
1. Checks if `PG_VERSION` file exists in the data directory
2. Compares data version with container's `POSTGRESQL_VERSION` env var
3. Sets `POSTGRESQL_UPGRADE=hardlink` only when versions differ
4. Calls `run-postgresql` to start the database

This handles all scenarios correctly:
- **Fresh install**: No data → normal initialization
- **Restart (same version)**: Versions match → normal startup
- **Upgrade (version mismatch)**: Versions differ → enables pg_upgrade

## How pg_upgrade Works

When `POSTGRESQL_UPGRADE=hardlink` is set and versions differ:

1. **Detect Version Mismatch**: The sclorg `run-postgresql` script reads `PG_VERSION` from the data directory
2. **Validate Source Version**: Checks that the data version matches `POSTGRESQL_PREV_VERSION` (e.g., PG13 image requires PG12 data)
3. **Run pg_upgrade**: Executes `pg_upgrade --link` to upgrade the data in-place using hardlinks
4. **Start PostgreSQL**: Normal postgres startup with upgraded data

### sclorg Environment Variables

The sclorg container images define these environment variables (baked into each image):

| Variable | Description | Example |
|----------|-------------|---------|
| `POSTGRESQL_VERSION` | Current PostgreSQL version | `13` |
| `POSTGRESQL_PREV_VERSION` | Previous version this image can upgrade from | `12` |

You can verify these by inspecting the container:
```bash
podman run --rm quay.io/sclorg/postgresql-13-c9s:latest env | grep POSTGRESQL
# POSTGRESQL_VERSION=13
# POSTGRESQL_PREV_VERSION=12
```

### Hardlink Mode

The `--link` flag tells `pg_upgrade` to create hardlinks instead of copying files:

- **Fast**: Completes in seconds regardless of database size
- **No Extra Storage**: Hardlinks share the same disk blocks as original files
- **Near-Atomic**: Hardlink creation is an atomic filesystem operation

## Preserving Events and Logs

If you need to ensure 100% preservation of events and logs, snapshot your database PVC before upgrading:

```bash
# Example: snapshot the PVC before MCE upgrade
kubectl get pvc postgres -n multicluster-engine -o yaml > postgres-pvc-backup.yaml
# Or use your storage class's snapshot feature if available
```

## Failure Handling

If the upgrade fails:

1. The postgres container crashes
2. Pod goes into `CrashLoopBackOff`
3. Logs show the error from sclorg/pg_upgrade
4. Manual investigation and recovery required

### Recovery Options

If upgrade fails and data is unrecoverable:

```bash
# 1. Check what went wrong
kubectl logs <pod-name> -c postgres -n multicluster-engine

# 2. If data is corrupt, delete the PVC to start fresh
kubectl delete pvc postgres-assisted-service -n multicluster-engine

# 3. Delete pod to force restart
kubectl delete pod <pod-name> -n multicluster-engine

# 4. New pod starts with fresh DB, controllers reconcile from CRs
```

Data loss on recovery:

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

Note: Upgrading from `postgresql-12-c8s` (RHEL 8) to `postgresql-13-c9s` (RHEL 9) is supported. See [Red Hat's fast upgrade documentation](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/configuring_and_using_database_servers/using-postgresql_configuring-and-using-database-servers#fast-upgrade-using-the-pg_upgrade-tool_migrating-to-a-rhel-9-version-of-postgresql).

## How to Upgrade PostgreSQL Version

To upgrade to a new PostgreSQL version:

1. Update `internal/controller/controllers/images.go` with the new image
2. Update `deploy/olm-catalog/manifests/assisted-service-operator.clusterserviceversion.yaml`:
   - Update `DATABASE_IMAGE` env var
   - Update `relatedImages` section
3. Update backplane-operator:
   - `hack/bundle-automation/config.yaml` - image mapping
   - `pkg/templates/charts/toggle/assisted-service/values.yaml`
   - `pkg/templates/charts/toggle/assisted-service/templates/infrastructure-operator.yaml`

The wrapper script automatically detects version mismatches and triggers `pg_upgrade` when needed.

## Deployment Strategy

The assisted-service deployment uses `Recreate` strategy (not `RollingUpdate`):

```go
deploymentStrategy := appsv1.DeploymentStrategy{
    Type: appsv1.RecreateDeploymentStrategyType,
}
```

This ensures the old pod releases the PVC before the new pod starts, preventing deadlocks.

## Version Skip Protection

The sclorg container validates that the source data version matches `POSTGRESQL_PREV_VERSION`. If a customer tries to skip versions (e.g., PG10 → PG13), the container fails with a clear error:

```
With this container image you can only upgrade from data directory
of version '12', not '10'.
```

This prevents accidental data corruption from unsupported upgrade paths.
