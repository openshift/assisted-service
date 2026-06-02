# Test Version Helpers

`TestVersion()` in `internal/common` resolves OCP versions, release image URLs, and RHCOS image URLs dynamically from the data files (`data/default_release_images.json` and `data/default_os_images.json`). Available versions and their associated URLs are auto-generated into `internal/common/test_versions_generated.go` as part of the standard `make generate` target.

## Scope

`TestVersion()` is required for **all** version references in **subsystem tests** (`subsystem/`). Subsystem tests run against the real service, so versions must resolve from the data files.

**Unit tests** may use hardcoded version strings since versions are not actually pulled from real sources. The one exception is the package-level defaults in `internal/common/test_configuration.go`, which use `TestVersion().Latest()` to set the mocked OpenShift version on the default unit test config (`TestDefaultConfig`).

## API

`TestVersion()` returns a `*TestVersionBuilder` that supports fluent chaining:

| Method | Description |
|--------|-------------|
| `.Latest()` | Selects the newest available version (default) |
| `.Oldest()` | Selects the oldest available version |

### Constraints

Constraints filter the available version list. Multiple constraints compose (all must pass).

| Method | Description |
|--------|-------------|
| `.GreaterThan(v)` | Versions strictly above `v` |
| `.GreaterThanOrEqual(v)` | Versions at or above `v` |
| `.LessThan(v)` | Versions strictly below `v` |
| `.LessThanOrEqual(v)` | Versions at or below `v` |
| `.Exact(v)` | Exactly `v` |

### Architecture

| Method | Description |
|--------|-------------|
| `.ForArch(arch)` | Filter to versions available for a specific architecture (default: x86_64) |
| `.AvailableForArches(arches...)` | Filter to versions that exist across all given architectures in addition to the one set on the builder |

### Terminal Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `.Version()` | `string` | Short version string (no patch), e.g. `"4.22"`. Panics if no match. |
| `.TryVersion()` | `(string, bool)` | Safe variant — returns `false` if no version matches |
| `.ReleaseVersion()` | `string` | Full release version from the data files, e.g. `"4.22.0"` |
| `.ReleaseImageURL()` | `string` | Release image URL from the data files |
| `.RhcosVersion()` | `string` | RHCOS version string from the data files |
| `.RhcosImageURL()` | `string` | RHCOS image URL from the data files |
| `.MultiVersion()` | `string` | Version with `-multi` suffix for multi-arch DB fields |
| `.MultiReleaseVersion()` | `string` | Multi-arch release version from the data files (includes `-multi` suffix) |
| `.MultiReleaseImageURL()` | `string` | Multi-arch release image URL from the data files |

All URL and version terminal methods return real values sourced from the data files. When the selected architecture has no standalone release entry, `ReleaseVersion()` and `ReleaseImageURL()` fall back to the multi-arch release entry. If no generated data matches at all, a computed fallback value is used.

`.Version()` may be used for unconstrained selections like `common.TestVersion().Version()`, where a match is guaranteed. `.TryVersion()` must be used when any constraints are applied, since matching versions may not be present.

## Patterns

### Version-agnostic

Tests that don't depend on a specific version boundary use `TestVersion().Version()` (defaults to latest x86).

```go
OpenshiftVersion: swag.String(common.TestVersion().Version()),
```

### Version boundary

Tests with version constraints express them with `TestVersion()` and fetch the version with `TryVersion()`, skipping when no matching version is present.

```go
// Test a feature introduced in 4.13 (e.g., vSphere platform support)
openShiftVersion, ok := common.TestVersion().GreaterThanOrEqual("4.13").TryVersion()
if !ok {
	Skip("No version >= 4.13 available")
}

// Test behavior for versions below a feature gate (e.g., before s390x support)
version, ok := common.TestVersion().LessThan("4.11").TryVersion()
if !ok {
	Skip("no available version without s390x support")
}

// Test behavior for an exact version (e.g., SNO CPU core requirements at 4.22).
// Exact() should only be used for boundary interactions specific to a single version.
// Most version boundaries should use GreaterThanOrEqual or LessThan instead.
version, ok := common.TestVersion().Exact("4.22").TryVersion()
if !ok {
	Skip("4.22 not available")
}
```

### CPU architecture

The builder defaults to x86_64 versions. `ForArch` changes the target CPU architecture. `AvailableForArches` filters to versions present across the given CPU architectures in addition to the builder's own.

```go
// Latest arm64 version
v := common.TestVersion().ForArch("arm64").Version()

// Latest version available on both x86_64 (default) and arm64
v := common.TestVersion().AvailableForArches("arm64").Version()

// Latest version available on both arm64 and s390x
v := common.TestVersion().ForArch("arm64").AvailableForArches("s390x").Version()
```

### Composite constraints

```go
// Selected version must match all constraints
v := common.TestVersion().
	ForArch("arm64").
	GreaterThanOrEqual("4.14").
	LessThanOrEqual("4.18").
	Version()
```

## Regeneration

When OCP versions are added or removed from `data/default_release_images.json` or `data/default_os_images.json`, run:

```bash
make generate
```

This regenerates `internal/common/test_versions_generated.go` with the current version-to-architecture mapping, including actual release image URLs/versions and RHCOS image URLs/versions from both data files.
