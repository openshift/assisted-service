# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Assisted Service is a REST/Kubernetes API service that installs OpenShift clusters with minimal infrastructure prerequisites. It supports highly-available control planes (3+ nodes) and Single-Node OpenShift (SNO). The service can operate in two modes:
- **REST API mode**: Standalone service exposing REST endpoints
- **Kube-API mode**: Operator exposing Kubernetes Custom Resources

## Architecture

### Core Components

1. **bminventory** (`internal/bminventory/`) - Main business logic and REST API handlers
   - Central orchestrator coordinating cluster and host operations
   - Implements REST API endpoints defined in `swagger.yaml`

2. **Cluster Management** (`internal/cluster/`) - Cluster lifecycle state machine
   - State transitions and validations for cluster installation
   - Works with cluster validation logic

3. **Host Management** (`internal/host/`) - Host lifecycle state machine
   - Manages individual host states and validations
   - Coordinates with hardware validation

4. **Controllers** (`internal/controller/`) - Kubernetes operator controllers
   - Reconcile CRs: AgentServiceConfig, InfraEnv, Agent, etc.
   - Only active in Kube-API mode

5. **Storage Layer** - PostgreSQL database via GORM
   - Models defined in `models/` (auto-generated from `swagger.yaml`)
   - Migration scripts in `internal/migrations/`

### Package Organization

- `internal/` - Core business logic (not importable by other projects)
  - `bminventory/` - Main API implementation
  - `cluster/`, `host/`, `infraenv/` - Domain logic
  - `hardware/`, `network/`, `connectivity/` - Validation logic
  - `operators/` - Operator support (OLM, LSO, etc.)
  - `controller/` - Kubernetes controllers

- `pkg/` - Reusable utilities (importable by other projects)
  - `auth/`, `db/`, `s3wrapper/`, `k8sclient/` - Infrastructure
  - `validations/`, `conversions/` - Domain utilities

- `restapi/` - Auto-generated REST server code (from `swagger.yaml`)
- `client/` - Auto-generated REST client code
- `api/` - Kubernetes API definitions (CRDs)
- `models/` - Auto-generated data models

## Common Development Commands

### Building

```bash
# Build everything (runs lint + unit tests + build)
skipper make all

# Build service binary only (skip validation)
skipper make build-minimal

# Build service container image
SERVICE=quay.io/<username>/assisted-service:<tag> skipper make build-image

# Build in current environment (no container)
make build-assisted-service
```

### Code Generation

After modifying `swagger.yaml`, regenerate code:

```bash
skipper make generate-from-swagger
```

This regenerates:
- `restapi/` - REST server code
- `client/` - REST client code
- `models/` - Data models

After modifying CRD definitions in `api/`:

```bash
make generate  # Generates deepcopy, CRD manifests, etc.
```

### Linting

```bash
skipper make lint        # Run all linters
skipper make format      # Auto-format code
```

## Testing

For comprehensive testing documentation, see:
- [Testing Overview](docs/dev/testing.md)
- [Running Subsystem Tests](docs/dev/running-test.md)

### System Requirements

#### Required Dependencies

1. **Go** (1.19+)
   - Check: `go version`

2. **Container Runtime** (for full unit tests)
   - **Docker** or **Podman**
   - Check: `docker --version` or `podman --version`
   - Used to run PostgreSQL test containers

3. **nmstate Development Headers** (for network state management tests)
   ```bash
   # Fedora/RHEL
   sudo dnf install -y nmstate-devel

   # Ubuntu/Debian
   sudo apt-get install -y libnmstate-dev
   ```

   **Note:** When using Claude Code, package installation commands must be run unsandboxed. Claude Code will prompt for permission when needed.

4. **gotestsum** (required for make test targets)
   - Required for `make unit-test` and `make run-unit-test`
   - Check: `which gotestsum`
   - Install:
     ```bash
     go install gotest.tools/gotestsum@latest
     export PATH=$PATH:$(go env GOPATH)/bin
     ```
   - **Note:** Tests will run for a long time before failing if gotestsum is not installed. Always verify it's available before running make test targets.

5. **Optional Tools**
   - `skipper` - Container-based build environment
   - `kind` - Kubernetes in Docker (for subsystem tests)

### Quick Start

#### Prerequisites Check

Before running the make test targets, verify your environment:

**1. Check for container runtime (Docker or Podman)**

The `make unit-test` and `make ci-unit-test` targets require a container runtime to run PostgreSQL. Check availability first:

```bash
# Check for Podman
which podman && podman --version

# OR check for Docker
which docker && docker --version
```

**Choose the appropriate make target based on availability:**
- **If Podman/Docker is available:** Use `make unit-test` for full test coverage with database tests
- **If NO container runtime:** Use `SKIP_UT_DB=1 make run-unit-test` to skip database-dependent tests

**2. Check for gotestsum**

The make targets depend on gotestsum, and tests will run for a long time before failing if it's missing:

```bash
# Check if gotestsum is installed
which gotestsum

# If not found, install it
go install gotest.tools/gotestsum@latest

# Ensure it's in your PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

### Unit Tests

#### Run All Unit Tests (Full)

```bash
# Requires Docker/Podman running
make unit-test
```

This will:
1. Start a PostgreSQL container
2. Run all unit tests
3. Kill the PostgreSQL container
4. Generate coverage reports

#### Run Unit Tests (Without Database)

If you don't have Docker/Podman available:

```bash
# Skip database-dependent tests
SKIP_UT_DB=1 make run-unit-test
```

Or directly with Go:

```bash
SKIP_UT_DB=1 go test ./... -count=1 -short
```

**Note:** Tests that require database access will fail in BeforeSuite with a clear "connection refused" error message (exit after ~5 seconds). Tests that don't require database access will pass normally. This is expected behavior when running without a database.

#### Make Targets

| Target | Description | Requirements |
|--------|-------------|--------------|
| `make unit-test` | Run all unit tests with DB | Docker/Podman, gotestsum |
| `make run-unit-test` | Run unit tests (with SKIP_UT_DB=1) | Go, gotestsum |
| `make ci-unit-test` | Run unit tests in CI mode | Docker/Podman, gotestsum |

**Important:** All make test targets require gotestsum. Check with `which gotestsum` before running.

#### Coverage Reports

```bash
# Run tests with coverage
make unit-test

# View coverage report
make display-coverage

# Coverage files are saved to:
# - reports/unit_coverage.out
# - reports/unit_coverage.xml (CI mode)
```

#### Running Specific Tests

##### By Package

```bash
# Single package
go test -v ./pkg/validations

# Multiple packages
go test -v ./pkg/validations ./pkg/webhooks/...

# All packages under a directory
go test -v ./internal/cluster/...
```

##### By Test Name

```bash
# Using -run flag (regex pattern)
go test -v ./pkg/validations -run TestValidateCluster

# Using Ginkgo focus
FOCUS="install_cluster" make run-unit-test
```

##### Skip Tests

```bash
# Using Ginkgo skip
SKIP="slow_test" make run-unit-test

# Skip subsystem tests
go test ./... -count=1 -short | grep -v subsystem
```

##### Packages Known to Pass Without Dependencies

These packages typically don't require database or nmstate:

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

### Subsystem Tests

Subsystem tests require a running Kubernetes cluster with the assisted-service deployed.

#### Prerequisites

- `podman` and `kind` in $PATH
- `skipper` tool
- `gotestsum` (required for running tests)

#### Setup and Run

```bash
# Install kind if needed
make install-kind-if-needed

# Deploy service for subsystem testing
make deploy-service-for-subsystem-test

# Run subsystem tests (REST-API mode)
skipper make subsystem-test

# Run subsystem tests (Kube-API mode)
ENABLE_KUBE_API=true make deploy-service-for-subsystem-test
skipper make subsystem-test-kube-api
```

#### Cleanup

```bash
make destroy-hub-cluster
```

See [Running Subsystem Tests](docs/dev/running-test.md) for more details.

### Test Categories

The project has three main test categories:

1. **Unit Tests** (this guide)
   - Fast, isolated tests
   - Mock external dependencies
   - Run with `make unit-test`

2. **Subsystem Tests** ([running-test.md](docs/dev/running-test.md))
   - Test service with mocked agent responses
   - Require Kubernetes cluster
   - Run with `skipper make subsystem-test`

3. **E2E Tests** (External repositories)
   - Full integration tests
   - Upstream: [assisted-test-infra](https://github.com/openshift/assisted-test-infra)
   - Downstream: QE maintained tests

### Environment Variables

#### Test Execution

| Variable | Description | Default |
|----------|-------------|---------|
| `SKIP_UT_DB` | Skip database container setup | unset |
| `TEST` | Specific test package(s) to run | all non-subsystem |
| `FOCUS` | Ginkgo focused specs (regex) | "" |
| `SKIP` | Ginkgo skip specs (regex) | "" |
| `VERBOSE` | Enable verbose output | false |
| `TIMEOUT` | Test timeout | 30m (unit), 120m (subsystem) |

#### Coverage

| Variable | Description | Default |
|----------|-------------|---------|
| `CI` | Enable CI mode (XML reports) | false |
| `COVER_PROFILE` | Coverage output file | reports/unit_coverage.out |
| `REPORTS` | Reports directory | ./reports |

#### Subsystem Tests

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVICE_IMAGE` | Custom service image | (local build) |
| `DEBUG_SERVICE` | Deploy in debug mode | false |
| `ENABLE_KUBE_API` | Enable Kube-API mode | false |

#### Examples

```bash
# Run specific package with verbose output
VERBOSE=true go test -v ./pkg/validations

# Run tests with custom timeout
TIMEOUT=60m make run-unit-test

# Run subsystem tests with focus
FOCUS="installation" skipper make subsystem-test

# Skip slow tests
SKIP="slow" make run-unit-test
```

### Troubleshooting

#### Missing nmstate.h

**Error:**
```
fatal error: nmstate.h: No such file or directory
```

**Solution:**
```bash
# Fedora/RHEL
sudo dnf install -y nmstate-devel

# Ubuntu/Debian
sudo apt-get install -y libnmstate-dev
```

**Note:** When using Claude Code, these package installation commands must be run unsandboxed. Claude Code will prompt for permission when needed.

**Workaround:** Tests for packages that don't depend on nmstate will still pass.

#### Cannot Connect to Docker Daemon

**Error:**
```
Cannot connect to the Docker daemon at unix:///var/run/docker.sock
```

**Solutions:**
1. Start Docker/Podman daemon
2. Use `SKIP_UT_DB=1` to skip database-dependent tests
3. Run specific test packages that don't need a database

#### Database Connection Errors

**Cause:** PostgreSQL container not running or can't connect

**Symptoms:** Tests fail in BeforeSuite with error message:
```
failed to connect to `host=127.0.0.1 user=postgres database=`:
dial tcp 127.0.0.1:5433: connect: connection refused
```

**Solutions:**
1. Ensure Docker/Podman is running
2. Use `SKIP_UT_DB=1` to skip database-dependent tests (tests will still fail fast with clear errors, but won't hang)
3. Manually start container:
   ```bash
   make run-db-container
   # Run your tests
   make kill-db-container
   ```

#### "gotestsum: command not found"

**Cause:** gotestsum not installed

**Solutions:**
1. Install: `go install gotest.tools/gotestsum@latest`
2. Use Go directly: `go test ./...`
3. Update PATH: `export PATH=$PATH:$(go env GOPATH)/bin`

#### Permission Denied on Docker Socket

**Cause:** User not in docker group

**Solutions:**
1. Add user to docker group: `sudo usermod -aG docker $USER`
2. Use podman instead: `export CONTAINER_COMMAND=podman`
3. Use `SKIP_UT_DB=1` to avoid needing containers

#### Tests Fail with "build failed"

**Cause:** Missing C dependencies (usually nmstate.h)

**Solutions:**
1. Install nmstate-devel: `sudo dnf install -y nmstate-devel`
   - **Note:** When using Claude Code, this command must be run unsandboxed. Claude Code will prompt for permission when needed.
2. Run tests for specific packages that don't need nmstate (see list above)
3. Use build tags to exclude problematic packages (advanced)

#### Tests Timeout

**Cause:** Default timeout too short for slow machines

**Solution:**
```bash
TIMEOUT=60m make run-unit-test
```

### Advanced Usage

#### Run with Coverage HTML Report

```bash
make unit-test
go tool cover -html=reports/unit_coverage.out -o reports/coverage.html
# Open reports/coverage.html in browser
```

#### Run Tests in CI Mode

```bash
CI=true make unit-test
# Generates XML reports in reports/ directory
```

#### Debug Tests

```bash
# Deploy service in debug mode
DEBUG_SERVICE=true make deploy-service-for-subsystem-test

# Run with verbose Ginkgo output
VERBOSE=true make run-unit-test
```

#### Quick Iteration During Development

```bash
# Terminal 1: Keep database running
make run-db-container

# Terminal 2: Run tests quickly
SKIP_UT_DB=1 go test -v ./pkg/your-package

# When done
make kill-db-container
```

## Deployment

### Local Development (Podman)

Lightest environment for REST API testing:

```bash
make deploy-onprem  # Starts DB, service, image-service, UI in pods

# Access UI: http://localhost:8080
# Access API: http://localhost:8090/api/assisted-install/v2/
```

Configuration in `deploy/podman/configmap.yml`.

### Kubernetes (kind)

Local Kubernetes environment with operator:

```bash
make deploy-dev-infra  # Creates kind cluster, deploys operator and services
```

### OpenShift

```bash
# Deploy with ingress
skipper make deploy-all TARGET=oc-ingress

# Optional parameters:
# APPLY_NAMESPACE=False - Skip namespace creation
# INGRESS_DOMAIN=apps.example.com - Specify domain
# DISABLE_TLS=true - Use HTTP routes
```

See `docs/dev/README.md` for all deployment scenarios.

## Key Patterns and Conventions

### State Machines

Cluster and Host entities use explicit state machines:
- Cluster states: `models.ClusterStatus*` constants
- Host states: `models.HostStatus*` constants
- Transitions handled by `cluster.API` and `host.API` interfaces
- State changes emit events via `eventsapi.Handler`

### Error Handling

- Use `pkg/error` for common error types
- Add context with `errors.Wrap()` from `github.com/pkg/errors`
- Log before returning errors: `log.WithError(err).Error("message")`

### Database Operations

- Use GORM ORM: `db.Model(&model).Where(...).Find(&results)`
- Transactions via `pkg/transaction` wrapper
- Always use preloading for associations: `.Preload("Hosts")`
- Soft deletes enabled on most models (check `DeletedAt` field)

### Logging

- Use logrus structured logging: `log.WithFields(logrus.Fields{...}).Info("message")`
- Request-scoped logger via `logutil.FromContext(ctx)`
- Include request ID: `pkg/requestid.FromContext(ctx)`

### Validation

- Pre-flight validations in `internal/cluster/validations`, `internal/host/validations`
- Hardware requirements in `data/default_hw_requirements.json`
- Network validations via `internal/connectivity`, `internal/network`

### API Versioning

- REST API: v1 (deprecated), **v2 (current)** - see `swagger.yaml`
- Kubernetes API: `v1beta1` - see `api/v1beta1/`

## Git Commit Guidelines

Commit messages should describe the change clearly. Pull request titles must reference a JIRA/GitHub issue (e.g., `MGMT-1234:`) or use `NO-ISSUE:` prefix.

For complete guidelines and examples, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Important Files

- `swagger.yaml` - REST API specification (source of truth for API)
- `Makefile` - Build targets
- `skipper.yaml` - Container build environment config
- `data/default_hw_requirements.json` - Hardware validation rules
- `data/default_must_gather_versions.json` - Must-gather image versions
- `deploy/` - Deployment manifests for various environments
- `docs/dev/` - Developer documentation
- `docs/user-guide/` - User-facing documentation

## CI/CD

The project uses Prow (primary) and Jenkins (secondary) for CI/CD. For details on CI jobs, debugging failures, and adding new jobs, see [docs/dev/testing.md](docs/dev/testing.md#repository-ci).

## Additional Resources

- [Testing Overview](docs/dev/testing.md) - Complete testing documentation
- [Running Subsystem Tests](docs/dev/running-test.md) - Detailed subsystem test guide
- [Debug Guide](docs/dev/debug.md) - Debugging assisted-service
- [Development Scenarios](docs/dev/README.md) - podman, kind, CRC, etc.
- [User Guide](docs/user-guide/README.md) - API usage and workflows
- [Contributing Guide](CONTRIBUTING.md) - PR process and commit guidelines
