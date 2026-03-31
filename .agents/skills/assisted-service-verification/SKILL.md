---
name: assisted-service-verification
description: "MANDATORY: Use this skill BEFORE any git commit, git push, amend, or claiming work is done in the assisted-service repo. Also use when the user asks to test, verify, build, lint, or validate changes. This skill MUST be read and followed before finishing any code change."
---

# Assisted Service Verification

**IMPORTANT: You MUST run at least steps 1-2 of the Quick Verification Checklist below BEFORE committing, amending, or pushing any changes. Do NOT skip verification before git operations.**

## Quick Verification Checklist

For most changes, run these in order (stop at first failure):

```
1. source .venv/bin/activate && skipper make build-minimal   # compilation check (ALWAYS use skipper)
2. source .venv/bin/activate && skipper make lint             # linters
3. SKIP_UT_DB=1 go test -v ./path/to/changed/package/...     # unit tests for changed packages
4. make unit-test                                             # full unit tests (optional, needs Docker/Podman + DB)
```

If generated code was modified (swagger, mocks, CRDs), also run:

```
5. source .venv/bin/activate && skipper make generate         # regenerate ALL
6. git diff --exit-code                                       # verify no generated code diff
```

## Critical Rules

- **NEVER run bare `go build`, `go test`, or `go vet` for compilation/lint** — they will fail because Go tooling, C headers (nmstate), and dependencies are only available inside the skipper container.
- **ALWAYS use `skipper make <target>`** for build, lint, and generate targets.
- **`go test` can be run directly** ONLY for unit tests of specific packages that don't need nmstate (see list below). Use `SKIP_UT_DB=1` if a DB container is already running at localhost:5433, or the test framework will try to create one.
- **ALWAYS activate the venv first**: `source .venv/bin/activate` before any `skipper` command.

## Prerequisites

Skipper requires the Python venv:

```bash
source .venv/bin/activate
```

See the `assisted-service-dev-mode` skill for venv setup if `.venv` doesn't exist.

## Build

```bash
source .venv/bin/activate
skipper make build-minimal      # build inside skipper container (ALWAYS use this)
```

## Code Generation

### Regenerate mocks only

```bash
skipper run 'go install go.uber.org/mock/mockgen@v0.6.0 && make generate-mocks'
```

Deletes all `mock_*.go` (outside vendor/) then runs `go generate` on all packages. The `mockgen` binary is not pre-installed in the skipper build container, so install it first with `go install` inside the same `skipper run` invocation. Use `skipper run '<commands>'` (not `skipper bash`) for arbitrary commands.

### Regenerate everything

```bash
skipper make generate
```

Runs in order: swagger, go mod tidy/vendor, events, mocks, config, OLM bundle.

### After modifying swagger.yaml

```bash
skipper make generate-from-swagger
```

### After modifying CRD types in api/

```bash
skipper make generate
```

## Linting

```bash
skipper make lint       # all linters
skipper make format     # auto-format (run before lint to reduce noise)
```

## Testing

### Fast feedback (no database required)

These packages pass without PostgreSQL or nmstate:

```bash
go test -v \
  ./pkg/app \
  ./pkg/conversions \
  ./pkg/error \
  ./pkg/filemiddleware \
  ./pkg/jq \
  ./pkg/kafka \
  ./pkg/log \
  ./pkg/mirrorregistries \
  ./pkg/requestid \
  ./pkg/s3wrapper \
  ./pkg/secretdump \
  ./pkg/thread \
  ./pkg/validations \
  ./pkg/webhooks/agentinstall/v1beta1 \
  ./pkg/webhooks/hiveextension/v1beta1 \
  ./internal/cluster/validations
```

### Full unit tests (requires Docker/Podman for PostgreSQL)

```bash
make unit-test
```

This starts PostgreSQL at localhost:5433, runs all tests, then stops the DB.

### Manual DB for repeated test runs

```bash
# Terminal 1: keep DB running
make run-db-container

# Terminal 2: run tests repeatedly
SKIP_UT_DB=1 go test -v ./internal/cluster/...
SKIP_UT_DB=1 go test -v ./internal/host/...

# When done
make kill-db-container
```

### Focused tests

```bash
go test -v ./internal/cluster -run TestClusterName    # by test name
FOCUS="install_cluster" make run-unit-test              # Ginkgo focus
```

## Verify Generated Code (CI Check)

CI runs `verify-generated-code` which does:

1. `make generate` (inside the `assisted-service-generate` container image)
2. `git diff --exit-code` — fails if any generated file differs from what's committed

To reproduce locally:

```bash
skipper make generate
git diff --exit-code
```

**Common causes of failure:**
- Import ordering: `goimports` (run during `generate`) re-sorts imports alphabetically. The `go.uber.org/` prefix sorts *after* `github.com/`, so `go.uber.org/mock/gomock` must appear after all `github.com/` imports within the same import group.
- Stale mocks: If you changed an interface but didn't regenerate mocks.
- Missing `go mod tidy`/`go mod vendor`: If you changed dependencies but didn't re-vendor.

**Always run `skipper make format` after bulk import changes** to let `gci` and `goimports` fix import ordering automatically.

## Dependency Change Verification

After adding, removing, or updating a Go dependency:

```
1. go mod tidy
2. go mod vendor
3. go build ./...
4. skipper make generate                  # regenerate everything
5. git diff --exit-code                   # verify no generated code diff
6. skipper make lint
7. go test <affected packages>
```
