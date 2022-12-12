---
title: auto-return-agent-to-infra-env
authors:
  - "@carbonin"
creation-date: 2022-05-17
last-updated: 2022-07-25
---

# Automatically Return Agents to InfraEnv

## Summary

Without a BareMetalHost there is currently no way to automate the process
of unbinding a host from a cluster. When a user wants to reuse a host for a new
cluster they have to use the assisted-service unbind API, then manually boot
the host back into the discovery image.

To support automating this procedure, the assisted-service will configure nodes
in installed clusters to run an agent. When appropriate this agent will
reconfigure the host to boot into a discovery image, allowing the host to
be made available for installation without the user manually interacting with
a management system to reboot the host into the discovery image.

## Motivation

A user creating and managing clusters using the assisted-service may not have
access to the systems used to configure and boot hosts. This is especially
true in bare-metal use cases when a BareMetalHost is not present. The
assisted-service can make this use case easier by handling the reprovisioning
process.

### Goals

- Reconfigure a host to boot the discovery image as a part of unbinding from a
  cluster without a BareMetalHost
- Do not run an agent on a node if it is not being unbound

### Non-Goals

- Automate graceful removal of a node from an existing cluster
- Booting a node that is unresponsive or not currently part of an installed
  cluster
- Registering an agent to an InfraEnv other than the one it is currently
  associated with
- Wiping a host's storage when it is removed from a cluster

## Proposal

When a host should be returned to the InfraEnv use the kubeconfig created when
the cluster was installed to run an agent on the host as a privileged DaemonSet
pod. This agent will handle downloading the required artifacts and rebooting
the host into the discovery process.

This process will only be used in situations where the assisted-service is sure
to have connectivity to the installed cluster's API endpoint. In cases such as
the SaaS this feature will not be used.

### Timeline

1. User unbinds the host from the cluster
2. Assisted service creates a DaemonSet to run the assisted-installer-agent
   on the host as a privileged pod

- If there is no BareMetalHost associated with the agent CR, the service
  executes a new host state transition (`TransitionTypeReclaimHost`)
- Host moves to state `HostStatusReclaiming`

3. The agent downloads the InfraEnv's kernel and initrd (including discovery
   ignition) to the host's `/boot` partition
4. The agent creates a boot entry to boot into the discovery kernel and initrd
   and posts a successful step response to the service

- Host moves to state `HostStatusReclaimingPendingReboot`

5. The agent reboots the host into the discovery image
6. Discovery agent registers host

- Service moves host to `HostStatusDiscoveringUnbound`

### User Stories

#### Story 1

As a cluster creator I want to use the assisted service to repurpose hosts
from existing clusters without requiring access to the host management
infrastructure (BMC, hypervisor, etc).

### Implementation Details/Notes/Constraints [optional]

- The boot artifact URLs (initrd, kernel, and rootfs) will be provided to the
  agent directly as a part of the pod spec that runs the agent.
- The agent will require two new commands; one for each of the new states
  1. Download boot artifacts and write boot entry (`HostStatusReclaiming`)
  2. Reboot the node (`HostStatusReclaimingPendingReboot`)
- The existing unbind API will be used to trigger this process
  - This means removing the cluster ref from the agent CR when using the kubeAPI

#### Example boot entry

```
$ cat /boot/loader/entries/00-assisted-discovery.conf
title Assisted Installer Discovery
version 999
options random.trust_cpu=on ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=http://10.1.155.24:6016/boot-artifacts/rootfs?arch=x86_64&version=4.10'
linux /boot/discovery/vmlinuz
initrd /boot/discovery/c4bd9751-f3ae-49ac-a36b-d3ffebef4a39-initrd.img
```

The name of this file and the version number ensure that it should always be the
first boot entry and will be selected upon reboot.

#### State Machine Changes

Currently when a host is installed, all unbind APIs will call the `UnbindHost`
transition. This will always move the host to
`HostStatusUnbindingPendingUserAction`. If the agent has a BMH associated with
it, the bare metal host agent controller (BMAC) will make the changes required
to boot the host back into discovery. If there is no BMH associated with the
agent (or the kube API is not in use), the host will remain in
`HostStatusUnbindingPendingUserAction`.

The following diagram illustrates the new proposed transition to indicate that a
running agent should execute the reclaim procedure.

```
                   -----------------------
                   | HostStatusInstalled |
                   -----------------------
                             |
                      User unbinds agent
                             |
     Kube API and no BMH     |   REST API, Kube API with BMH, or other machine management
          -------------------+-------------------
          |                                     |
          |                                     V
-----------------------------            ----------------------------
| TransitionTypeReclaimHost |            | TransitionTypeUnbindHost |----------------------
-----------------------------            ----------------------------                     |
          |                                                                               |
          V                                                                               |
  ------------------------                                                                |
  | HostStatusReclaiming |-------------------------------------                           |
  ------------------------                                    |                           |
       |        |                                             |                           |
       |        |                                             |                           |
       |        ---------------------------                   |                           |
       |                                  |                   |                           |
    Success                               |                Failure                        |
       |                                  |                   |                           |
-------------------------------------     |                   |                           |
| TransitionTypeRebootingForReclaim |     |                   |                           |
-------------------------------------     |                   |                           |
       |                                  |                   |                           |
       V                                  |                   |                           |
---------------------------------         |                   |                           |
| HostStatusReclaimingRebooting |---------+                   |                           |
---------------------------------      Timeout                |                           |
                |                         |                   |                           |
------------------------------ ------------------------- -------------------------------  |
| TransitionTypeRegisterHost | | TransitionTypeRefresh | | TransitionTypeReclaimFailed |  |
------------------------------ ------------------------- -------------------------------  |
                |                         |                   |                           |
                V                         V                   V                           |
--------------------------------  ----------------------------------------                |
| HostStatusDiscoveringUnbound |  | HostStatusUnbindingPendingUserAction |<----------------
--------------------------------  ----------------------------------------
              ^                                       |
              |       ------------------------------  |
              --------| TransitionTypeRegisterHost |---
                      ------------------------------
```

#### Assumptions

- This approach only works if the host is currently part of a working cluster
  - This cluster needs to have been installed by assisted service so that
    it has access to the kubeconfig
- This reclaim procedure will only be triggered if assisted service is running
  the kubernetes API controllers
  - Assisted service needs to ensure the host isn't being managed by a bare
    metal host (only possible with the kubernetes API)
  - When using the REST API there is no way for a user to indicate if a host
    should be unbound normally or if it should be reclaimed
- There must be enough free space on the `/boot` partition
  - Space is required to store the initrd and kernel (~90MB)
  - The agent can check for this
- A node in a spoke cluster can be identified by the hostname in the agent
  - In a non-CAPI scenario there are no other reliable means to identify which
    node maps to which agent
  - With CAPI a string of references exist that could be used to identify the
    node, but the hostname should be reliable in this case as well

### Risks and Mitigations

It's possible that something could go wrong during the boot process. A user
that doesn't have access to the host infrastructure wouldn't be able to debug
or solve this type of problem.

A timeout will be implemented when the service expects the host to reregister
(in `HostStatusReclaimingPendingReboot`). After this time the host will move to
the existing `HostStatusUnbindingPendingUserAction` state.

If the node hasn't been removed from the cluster when the host reboots the host
could boot back into the node and the pod running the agent would attempt to
register. The assisted service would not know the difference between this
scenario and if the node had booted into discovery. To deal with this, the
next-step-runner process will be started in the pod rather than the full agent
as the only difference is the registration step.

If the node being reclaimed is being managed by a machine API provider in the
installed cluster it doesn't make much sense for assisted to be attempting to
manage it as well. If a node is unexpectedly removed from a cluster with
machine management it's likely that the underlying provider will remove the
infrastructure behind that node rendering it unusable by assisted. Assisted
service should attempt to determine if the node is under some other management
before starting the agent. If it is, proceed as before and move the node to
`HostStatusUnbindingPendingUserAction`.

## Design Details [optional]

### Open Questions

### UI Impact

No UI changes will be required as this procedure will be triggered using the
existing unbind action.

### Test Plan

Any existing end to end late binding test could be expanded to include
returning the agents to the infraEnv after a successful install.

## Drawbacks

This will only work in environments where the service has access to the
kubernetes API on the cluster and where we using the agent kubernetes API.
This specifically means that it won't be usable in the SaaS.

## Alternatives

1. Install an agent when the cluster is first installed

- This would be a full polling approach where the agent is always polling the
  service to determine if it should reprovision.
- This is likely not worth the resources and clusters don't exist long enough in
  the cloud for this to work currently.

2. Implement starting the agent in MAPI and/or CAPI

- These components are already the starting point for removing a node from a
  cluster and would likely be the ones triggering the unbind
- In this case the assisted-service wouldn't require the spoke cluster
  kubeconfig as MAPI/CAPI should already have this access.
- A lot of information will be required to properly start the agent
  - Agent image pull spec
  - Hub cluster CA cert
  - http(s) proxy information
  - Agent authentication information (pull secret)
  - Boot artifact URLs
- There's not much relevant difference between doing any of this in the CAPI
  provider vs assisted
- Adding a MAPI provider would involve creating our own platform type which
  is not something we want to take on
