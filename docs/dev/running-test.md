# Running Assisted-service Subsystem Tests on Kubernetes

## Overview

This document details the steps required to run subsystem tests for the Assisted-service deployed in two modes:
- REST-API mode
- Kube-API mode

## Deployment for Subsystem Tests

Assisted-service components will be deployed in a Kind cluster using the Podman provider. 
`podman` and `kind` and `skipper` executables must be available in the $PATH.
you can install kind by running:

```bash
make install-kind-if-needed
```
which will install `kind` latest version if it is not present.

## Deploying Components

To deploy in REST-API mode, run:

```bash
make deploy-service-for-subsystem-test
```

To deploy kube-api mode, run:

```bash
ENABLE_KUBE_API=true make deploy-service-for-subsystem-test
```

Optionally the following environment variables can be exported:

* `SERVICE_IMAGE` - Specify external image to use for assisted-service.
* `DEBUG_SERVICE=true` - Deploy assisted-service for subsystem tests in debug mode. In order to debug assisted-service code after it is deployed, you need need to connect to dlv, see [Debug](debug.md). Once you are connected, set break points and run the tests. **Note**: you can only debug the code executed inside assisted-service container this way (not the test code, etc.)

## Running the tests

When the components are already deployed, you can run the tests as many times as you like:

To test in REST-API mode, run:

```bash
skipper make subsystem-test
```

To test kube-api mode, run:

```bash
skipper make subsystem-test-kube-api
```

Optionally the following environment variables can be exported:

* `FOCUS="install_cluster"` - An optional flag used for [focused specs](https://onsi.github.io/ginkgo/#focused-specs) with regular expression.
* `SKIP="install_cluster"` - An optional flag to skip scopes with regular expressions.
* `VERBOSE=true` - An optional flag to print verbosed data.

## Quick Update and Test

To quickly update the service and run tests after making code changes, use:

```bash
make patch-service
```

This command builds a new service image, pushes it to the container registry, and triggers a rollout of the updated service Deployment.

## Cleanup

to Destroy the cluster, run:

```bash
make destroy-kind-cluster
```
