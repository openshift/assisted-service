---
title: boot-status-reporter
authors:
  - "@nmagnezi"
creation-date: 2022-09-22
last-updated: 2022-09-22
---

# Assisted Boot Reporter

## Summary

Create a new reporting service that runs as early as possible when hosts are booted from disk to collect additional 
information and send it to `assisted-service`.

## Motivation

There are multiple occurrences of clusters that fail to install at the stage where
hosts manage to pull ignition but fail to get to a point where the `assisted-controller`
pod runs and calls home to `assisted-service` with logs.

With that missing information, there's not a whole lot we can do to understand what
happened with those installations.

A new service that starts right after the boot process on all cluster hosts with no dependency on `assisted-controller`
or `crio` will report logs we don't currently have and shed some light on why the host failed to progress and
what exactly happened.


### Goals

1. Gather information for a host boot stage to improve our ability to debug failures and better inform 
   users about the host progress stages (first boot, second boot, etc).

2. The new reporting service should run independently on all cluster hosts, regardless of their role.

3. The new service should be thoroughly cleaned up from the hosts and should never fail installations by itself.
   If the boot reporter service fails to run or report logs, the cluster installation should continue, 
   and the new host progress stages should be skipped in such a scenario.

## Proposal


### Changes To Assisted Service
For cases as mentioned in `Motivation`, we need a new service (started via `systemd`) as soon as possible after hosts
are booting from the disk and contact `assisted-service` to:

1. Change the installation stage to a new `Booted with local ignition` between `Rebooting` and `Configuring`.
   This stage refers to a boot with pointer ignition.

2. Change the installation stage to a new `Booted with control plane ignition` Between
   `Waiting for control plane` and  `Joined`.


3. For each of the above-mentioned, Read the current host stage from `assisted-service`, to determine if:
   - the service runs on the first boot :arrow_right: collect logs and report.
   - or the second boot :arrow_right: collect logs, report, and self-cleanup after a configurable threshold.

4. Add a new `LogsType` so the collected logs won't override the existing logs.
   - current names: `host`, `controller`, `all`
   - proposing to add a new name: `host_boot`

### Information Needed And Changes To The Discovery Agent

In order to communicate with `assisted-service`, The assisted boot reported service needs to know:
1. The `assisted-service` URL.
2. The pull-secret to use for agent authentication.
3. The InfraEnv ID and host ID

It will need this information starting from the first boot, which is a pointer ignition-based boot.

For that, we need to modify the discovery agent to store this information on disk in a pre-defined location for the assisted boot reporter service to use.

The assisted boot reporter service should start via `systemd` and determine its operating stage. As mentioned above:

### First Boot (Pointer Ignition)
Expected stage to be read from assisted-service backend: `Rebboting`.
Update the host stage to: `Booted with local ignition`.
Start a timer to compress and send logs every 10 minutes to `v2UpdateHostInstallProgress` API endpoint.


### Second Boot (Control Plane Ignition)
Expected stage to be read from assisted-service backend: `Waiting for control plane`.
Update the host stage to: `Booted with control plane ignition`.
Start a timer to compress send logs every 10 minutes to `v2UpdateHostInstallProgress` API endpoint.
Start another timer to halt operation and self-cleanup as follows:

1. If 1 hour has passed.
2. If cluster install progress reached a point where it is no longer needed (now counting in `assisted-controller` pod). An indication could be that `kubelet` is up and running.
   Cleanup should include:

   * The stored files for pull-secret, host id, and infraEnv id.
   * Service itself - should not run on additional host boot.

### Collected Information

Kept under a new logs type: `host_boot`:

1. journalctl
2. A list of mounted file systems
3. crio logs
4. network settings


### Open Questions

1. What should we use for implementation? Our options are:
   - Bash:
      - Advantages:
          1. Has very little to no dependencies.
          2. Expected to be present in future versions of RHCOS.
      - Disadvantages:
         1. Harder to maintain and test.
         2. No code discovery agent code reuse (written in Go).
         3. Client work will involve a lot of parsing.
   - Go:
      - Advantages:
         1. Discovery agent code reuse (also written in Go).
         2. Simpler to test and maintain, as we do for other assisted code bases. 
         3. Can be shipped as a binary, which helps with dependencies.
      - Disadvantages:
         1. Not clear if we can include a Go binary in the ignition.
         2. Multi-Platform support: we need to have a statically-linked binary not to introduce
            more dependencies to the platform. Note that it needs to compile for any platform that
            we want to support (x86, ARM, etc.)

   - Python:
      - Advantages:
         1. Code reuse to some extent. Swagger is able to generate a Python-based client. 
         2. Simpler to test and maintain, comparing to bash.
      - Disadvantages:
         1. Python is not included with RHCOS. We will need to install it and some third-party python libs it will probably need (e.g., python-requests).


2. How to handle pending user action state?

3. Are there any SNO specific things to take into account?

4. Logs should be compressed before we send them. Any limitation with what we have installed on RHCOS?
### UI Impact

UI is expected to present the newly added host stages. Those will get reflected by the backend in the same way as the 
current host stages.