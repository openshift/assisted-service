# Assisted Service - Testing Guide

Quick reference for running tests in the assisted-service project.

For comprehensive testing documentation, see:
- [Testing Overview](docs/dev/testing.md)
- [Running Subsystem Tests](docs/dev/running-test.md)

## Table of Contents

- [System Requirements](#system-requirements)
- [Quick Start](#quick-start)
- [Unit Tests](#unit-tests)
- [Subsystem Tests](#subsystem-tests)
- [Running Specific Tests](#running-specific-tests)
- [Environment Variables](#environment-variables)
- [Troubleshooting](#troubleshooting)

## System Requirements

### Required Dependencies

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

## Quick Start

### Prerequisites Check

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

### Run All Unit Tests (Full)

```bash
# Requires Docker/Podman running
make unit-test
```

This will:
1. Start a PostgreSQL container
2. Run all unit tests
3. Kill the PostgreSQL container
4. Generate coverage reports

### Run Unit Tests (Without Database)

If you don't have Docker/Podman available:

```bash
# Skip database-dependent tests
SKIP_UT_DB=1 make run-unit-test
```

Or directly with Go:

```bash
SKIP_UT_DB=1 go test ./... -count=1 -short
```

**Note:** Some tests will fail without a database, but many will pass.

## Unit Tests

### Make Targets

| Target | Description | Requirements |
|--------|-------------|--------------|
| `make unit-test` | Run all unit tests with DB | Docker/Podman, gotestsum |
| `make run-unit-test` | Run unit tests (with SKIP_UT_DB=1) | Go, gotestsum |
| `make ci-unit-test` | Run unit tests in CI mode | Docker/Podman, gotestsum |

**Important:** All make test targets require gotestsum. Check with `which gotestsum` before running.

### Coverage Reports

```bash
# Run tests with coverage
make unit-test

# View coverage report
make display-coverage

# Coverage files are saved to:
# - reports/unit_coverage.out
# - reports/unit_coverage.xml (CI mode)
```

### Common Issues

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

## Subsystem Tests

Subsystem tests require a running Kubernetes cluster with the assisted-service deployed.

### Prerequisites

- `podman` and `kind` in $PATH
- `skipper` tool
- `gotestsum` (required for running tests)

### Setup and Run

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

### Cleanup

```bash
make destroy-hub-cluster
```

See [Running Subsystem Tests](docs/dev/running-test.md) for more details.

## Running Specific Tests

### By Package

```bash
# Single package
go test -v ./pkg/validations

# Multiple packages
go test -v ./pkg/validations ./pkg/webhooks/...

# All packages under a directory
go test -v ./internal/cluster/...
```

### By Test Name

```bash
# Using -run flag (regex pattern)
go test -v ./pkg/validations -run TestValidateCluster

# Using Ginkgo focus
FOCUS="install_cluster" make run-unit-test
```

### Skip Tests

```bash
# Using Ginkgo skip
SKIP="slow_test" make run-unit-test

# Skip subsystem tests
go test ./... -count=1 -short | grep -v subsystem
```

### Packages Known to Pass Without Dependencies

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

## Environment Variables

### Test Execution

| Variable | Description | Default |
|----------|-------------|---------|
| `SKIP_UT_DB` | Skip database container setup | unset |
| `TEST` | Specific test package(s) to run | all non-subsystem |
| `FOCUS` | Ginkgo focused specs (regex) | "" |
| `SKIP` | Ginkgo skip specs (regex) | "" |
| `VERBOSE` | Enable verbose output | false |
| `TIMEOUT` | Test timeout | 30m (unit), 120m (subsystem) |

### Coverage

| Variable | Description | Default |
|----------|-------------|---------|
| `CI` | Enable CI mode (XML reports) | false |
| `COVER_PROFILE` | Coverage output file | reports/unit_coverage.out |
| `REPORTS` | Reports directory | ./reports |

### Subsystem Tests

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVICE_IMAGE` | Custom service image | (local build) |
| `DEBUG_SERVICE` | Deploy in debug mode | false |
| `ENABLE_KUBE_API` | Enable Kube-API mode | false |

### Examples

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

## Troubleshooting

### Tests Fail with "build failed"

**Cause:** Missing C dependencies (usually nmstate.h)

**Solutions:**
1. Install nmstate-devel: `sudo dnf install -y nmstate-devel`
   - **Note:** When using Claude Code, this command must be run unsandboxed. Claude Code will prompt for permission when needed.
2. Run tests for specific packages that don't need nmstate (see list above)
3. Use build tags to exclude problematic packages (advanced)

### Tests Timeout

**Cause:** Default timeout too short for slow machines

**Solution:**
```bash
TIMEOUT=60m make run-unit-test
```

### Database Connection Errors

**Cause:** PostgreSQL container not running or can't connect

**Solutions:**
1. Ensure Docker/Podman is running
2. Use `SKIP_UT_DB=1` to skip database tests
3. Manually start container:
   ```bash
   make run-db-container
   # Run your tests
   make kill-db-container
   ```

### "gotestsum: command not found"

**Cause:** gotestsum not installed

**Solutions:**
1. Install: `go install gotest.tools/gotestsum@latest`
2. Use Go directly: `go test ./...`
3. Update PATH: `export PATH=$PATH:$(go env GOPATH)/bin`

### Permission Denied on Docker Socket

**Cause:** User not in docker group

**Solutions:**
1. Add user to docker group: `sudo usermod -aG docker $USER`
2. Use podman instead: `export CONTAINER_COMMAND=podman`
3. Use `SKIP_UT_DB=1` to avoid needing containers

## Advanced Usage

### Run with Coverage HTML Report

```bash
make unit-test
go tool cover -html=reports/unit_coverage.out -o reports/coverage.html
# Open reports/coverage.html in browser
```

### Run Tests in CI Mode

```bash
CI=true make unit-test
# Generates XML reports in reports/ directory
```

### Debug Tests

```bash
# Deploy service in debug mode
DEBUG_SERVICE=true make deploy-service-for-subsystem-test

# Run with verbose Ginkgo output
VERBOSE=true make run-unit-test
```

### Quick Iteration During Development

```bash
# Terminal 1: Keep database running
make run-db-container

# Terminal 2: Run tests quickly
SKIP_UT_DB=1 go test -v ./pkg/your-package

# When done
make kill-db-container
```

## Test Categories

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

## Additional Resources

- [Testing Overview](docs/dev/testing.md) - Complete testing documentation
- [Running Subsystem Tests](docs/dev/running-test.md) - Detailed subsystem test guide
- [Debug Guide](docs/dev/debug.md) - Debugging assisted-service
- [Contributing Guide](CONTRIBUTING.md) - Contribution guidelines
