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
- **Fresh install**: No data → hop init containers no-op, main container initializes
- **Restart (same version)**: Versions match → hop init containers no-op, normal startup
- **Adjacent upgrade (version mismatch of one hop)**: Main container enables pg_upgrade
- **EUS skip-upgrade (for example PG12 → PG15 or PG12 → PG16)**: Init containers walk the sclorg hop chain, then the main container starts

## EUS skip-upgrades (ACM-39262)

Each sclorg image can only upgrade from `POSTGRESQL_PREV_VERSION`. ACM EUS skips more than one PostgreSQL major version:

| ACM upgrade | Data version | Target image | Required hops |
|-------------|--------------|--------------|---------------|
| 2.15 → 2.16 | PG12 | PG13 | 12 → 13 |
| 2.16 → 2.17 | PG13 | PG15 | 13 → 15 |
| 2.15 → 2.17 | PG12 | PG15 | 12 → 13 → 15 |
| 2.16 → 2.18 | PG13 | PG16 | 13 → 15 → 16 |
| 2.15 → 2.18 | PG12 | PG16 | 12 → 13 → 15 → 16 |

The operator therefore adds **init containers** for every hop strictly older than `DATABASE_IMAGE`:

1. `postgres-upgrade-to-13` uses `DATABASE_IMAGE_PG13` (`postgresql-13-c9s`) and runs `postgres_hop.sh`
2. `postgres-upgrade-to-15` uses `DATABASE_IMAGE_PG15` (`postgresql-15-c9s`) and runs `postgres_hop.sh`
3. The main `postgres` container uses `DATABASE_IMAGE` and `postgres_startup.sh`

`postgres_hop.sh` only reads `PG_VERSION` to decide whether to run. If data is already at or newer than that hop, it exits 0 without starting PostgreSQL (an older image cannot open a newer data directory). If data matches `POSTGRESQL_PREV_VERSION`, it sets `POSTGRESQL_UPGRADE=hardlink`, calls sclorg `try_pgupgrade`, and exits so the next container can run.

These intermediate images **must** be in the operator CSV `relatedImages` and operator env (`DATABASE_IMAGE_PG13`, `DATABASE_IMAGE_PG15`) so disconnected / oc-mirror payloads include them.

If the target image reference has no `postgresql-N` token (bare digest), set `DATABASE_IMAGE_VERSION` to the major version (for example `16`) so the operator still emits the correct hops. When the image name already contains `postgresql-N`, that token wins over `DATABASE_IMAGE_VERSION`.

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
podman run --rm quay.io/sclorg/postgresql-16-c9s:latest env | grep POSTGRESQL
# POSTGRESQL_VERSION=16
# POSTGRESQL_PREV_VERSION=15
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

Note: The sclorg images support cross-OS upgrades (e.g., RHEL 8 to RHEL 9). See [Red Hat's fast upgrade documentation](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/configuring_and_using_database_servers/using-postgresql_configuring-and-using-database-servers#fast-upgrade-using-the-pg_upgrade-tool_migrating-to-a-rhel-9-version-of-postgresql).

Note: There is no `postgresql-14-c9s` image. RHEL 9 module streams skip PG14, so the upgrade path goes 13 → 15 → 16 → 17.

## How to Upgrade PostgreSQL Version

To upgrade to a new PostgreSQL version:

1. Update `internal/controller/controllers/images.go` with the new image
2. Add the previous image as a hop in `postgres_upgrade.go` (`postgresUpgradePath`) if customers can skip to the new version from an older EUS
3. Update operator packaging so `hack/generate.sh generate_bundle` keeps the hop images:
   - `config/manager/manager.yaml`: `DATABASE_IMAGE` plus `DATABASE_IMAGE_PG<prev>` env vars
   - `config/manifests/bases/assisted-service-operator.clusterserviceversion.yaml`: same env vars and `relatedImages`
   - `config/manifests/bundle-overrides/related-images-patch.yaml`: current image plus every hop image
     (operator-sdk drops `relatedImages`; this patch is what verify-generated-code checks)
   - Then regenerate the catalog CSV (`deploy/olm-catalog/manifests/assisted-service-operator.clusterserviceversion.yaml`)
4. Update backplane-operator:
   - `hack/bundle-automation/config.yaml` - image mapping
   - `pkg/templates/charts/toggle/assisted-service/values.yaml`
   - `pkg/templates/charts/toggle/assisted-service/templates/infrastructure-operator.yaml`

Hop init containers plus the wrapper script automatically walk the sclorg path and trigger `pg_upgrade` when needed.

## Deployment Strategy

The assisted-service deployment uses `Recreate` strategy (not `RollingUpdate`):

```go
deploymentStrategy := appsv1.DeploymentStrategy{
    Type: appsv1.RecreateDeploymentStrategyType,
}
```

This ensures the old pod releases the PVC before the new pod starts, preventing deadlocks. Hop init containers run in that new pod, sequentially, on the same PVC.

## Unsupported source versions

The sclorg container still validates that each hop's source data matches `POSTGRESQL_PREV_VERSION`. If data is older than the first hop we ship (currently PG12 via `postgresql-13-c9s`), the hop init container fails with:

```text
this hop image can only upgrade from 12, not '<version>'.
An earlier hop init container is missing from the upgrade path.
```
