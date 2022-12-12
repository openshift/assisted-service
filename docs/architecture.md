# Architectural Overview

- [Introduction](#introduction)
- [File Storage](#file-storage)
- [State Machines](#state-machines)
  - [Host State Machine](#host-state-machine)
  - [Cluster State Machine](#cluster-state-machine)
- [Discovery Image Generation](#discovery-image-generation)
- [Agent](#agent)
- [Installation flow](#installation-flow)

## Introduction

The Assisted Service contains logic for handling API requests as well as several periodic tasks that run in the background. It exposes both a REST API as well as a Kubernetes API implemented via [Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/). The REST API for the service is described in OpenAPI/Swagger 2.0 in this repository ([raw](https://raw.githubusercontent.com/openshift/assisted-service/master/swagger.yaml), [HTML](https://generator.swagger.io/?url=https://raw.githubusercontent.com/openshift/assisted-service/master/swagger.yaml)).

The main resources in the REST API are:

- Cluster: A definition of an OpenShift cluster, along with its current installation state and progress
- Host: A host that is associated with a cluster resource, which like the cluster resource includes its current installation state and progress. It also includes a description of its hardware inventory and current connectivity information.
- Image: The definition of a bootable image that is used for host discovery. Once the image info is specified, a URL from where the image may be downloaded can be fetched from the API.

```
------------    -----------
| REST API |    | k8s API |
---------------------------
|      Service logic      |
---------------------------
      |              |
      V              V
--------------   ----------
| file store |   | SQL DB |
--------------   ----------
```

## File Storage

As can be seen in the elegant diagram above, the service requires storage for files which include: a cache of RHCOS images that the service uses for boot image generation, various Ignition configuration files, as well as log files. The service can be configured to use an S3 bucket or local storage for some of these files, the RHCOS images are always stored locally with the image service. S3 is generally used when deploying the Assisted Service in the cloud, while using directories on a file system is used when deploying the service as an operator (a Persistent Volume should be used). Additionally, the service requires an SQL database to store metadata about the OpenShift clusters being installed and the hosts that comprise them.

## State Machines

Each cluster and each host being installed moves through their respective state machines that are defined in the service. A cluster or host can transition its state either via user action, or via periodic monitor tasks that run in the service and determine the appropriate state.

### Host State Machine

![host state machine](https://raw.githubusercontent.com/openshift/assisted-service/master/docs/HostStatus.png)

- Discovering: Initial state where the host agent sends hardware and connectivity information.
- Pending-for-input: The user should input some configuration information so that the service can validate and move the host to “known” or “insufficient” state.
- Known: Hardware and link information is known and sufficient.
- Insufficient: One or more host validations is failing, for example the hardware or connectivity is not sufficient. Hosts in this state must either be fixed or disabled to continue with the installation.
- Disconnected: The host has not sent a ping to the service for some time (3 minutes). Hosts in this state must either be fixed or disabled to continue with the installation.
- Disabled: The user has selected to disable this host. Hosts in this state will not participate in the installation.
- Installation states: Triggered once the user initiates installation.
  - Preparing-for-installation: The service runs openshift-install create ignition-configs and uploads all files to S3. If the user chose to use route53 for DNS, the service creates those record sets.
  - Installing: The service is ready to begin the cluster installation. Next time the agent asks for instructions, the service will instruct it to begin the installation, and then moves the state to installing-in-progress.
  - Installing-in-progress: The host is currently installing.
  - Installing-pending-user-action: If the service expected the host to reboot and boot from disk, but the agent came up again and contacted the service, the host enters this state to notify the user to fix the server’s boot order.
- Resetting: If the user requested to reset the installation, the host enters this transient state while the service resets.
  - Resetting-pending-user-action: To reset the installation, the host needs to be booted from the live image. If the host already booted from disk in a previous installation, the host enters this state to notify the user to boot from the live image.
- Installed: The installation has successfully completed on the host.
- Error: The installation has failed.

### Cluster State Machine

![cluster state machine](https://raw.githubusercontent.com/openshift/assisted-service/master/docs/ClusterStatus.png)

- Pending-for-input: The user should input some configuration information so that the service can validate and move the cluster to “ready” or “insufficient” state.
- Insufficient: One or more cluster validations is failing.
- Ready: The cluster is ready for the user to request the installation to start.
- Preparing-for-installation: Same as hosts’s preparing-for-installation state.
- Installing: Cluster is currently installing.
- Finalizing: Cluster is formed, waiting for components to come up.
- Installed : Cluster installed successfully.
- Error: Error during installation.

The installation will be marked successful if all control plane nodes were deployed successfully, and if at least 2 worker nodes were deployed successfully (in case the cluster definition specified worker nodes).

## Discovery Image

The Assisted Service can currently be configured to provide two types of ISOs, full and minimal, both based on [Red Hat Enterprise Linux CoreOS](https://access.redhat.com/documentation/en-us/openshift_container_platform/4.7/html/architecture/architecture-rhcos) (RHCOS). A live ISO is used, such that everything is run from memory, until an RHCOS image is written to disk and the host is rebooted during installation.

The full ISO is simply an RHCOS live ISO with an [Ignition](https://access.redhat.com/documentation/en-us/openshift_container_platform/4.7/html/architecture/architecture-rhcos#rhcos-about-ignition_architecture-rhcos) config embedded in it, which includes information such as the cluster ID, the user's pull secret (used for authentication), as well as the service file to start the agent process.

The minimal ISO is significantly smaller in size due to the fact that the `rootfs` is downloaded upon boot rather than being embedded in the ISO. This ISO format is especially useful for booting via Virtual Media over a slow network, where the rootfs can later be download over a faster network. Other than the Igntion config that is embedded similarly to the full ISO, network configuration (e.g., static IPs, VLANs, bonds, etc.) is also embedded so that the rootfs can be downloaded at an early stage.

## Image Service

The discovery image is served from a separate service, the Assisted Image Service.
This service communicates back to the Assisted Service to fetch required information when a user downloads an image. It then streams the customized image directly to the user.

It always uses local storage to cache the "base" RHCOS images into which the installation configuration is embedded.

## Agent

When a host is booted with a discovery image, an agent automatically runs and registers with the Assisted Service. Communication is always initiated by the agent, as the service may not be able to contact the hosts being installed. The agent contacts the service once a minute to receive instructions, and then posts the results as well. The instructions to be performed are based on the host's state, and possibly other properties. See [below](#host-state-machine) for a description of the various host states.

## Installation flow

When the installation is started, all hosts are still booted from the live ISOs and have agents running which are periodically contacting the Assisted Service for instructions.

The first thing that the Assisted Service does when installation is initiated is compile an install-config.yaml, and then run the OpenShift installer to generate the ignition configs and place them in the file storage. At this point the service will also validate the installation disk speed on all hosts (this test writes to the disk so it is not performed before the user initiates the installation).

OpenShift installation generally requires a temporary host to be allocated during installation to run the bootstrap logic. The Assisted Service does not require an additional host, but instead one of the control plane nodes is randomly selected to run bootstrap logic during the installation.

The installation flow for a host that isn't running the bootstrap logic is as follows:

1. Fetch the relevant ignition file from the service's REST API.
1. Run `coreos-installer` to write the relevant RHCOS image and ignition to disk (1st ignition that will point to API VIP).
1. Trigger host reboot.
1. The host will start with the new RHCOS image and ignition, and will contact the `machine-config-server` running on the bootstrap host in order to complete the installation.
1. The nodes will get approved by the csr-approver service running on the bootstrap host.

The flow for the host running the bootstrap logic is as follows:

1. Fetch the bootstrap ignition file from the REST API.
1. Run the MCO container for writing the configuration to disk (using `once-from` option).
1. Copy assisted-controller deployment files to manifests folder (/opt/openshift/manifests). The `assisted-controller` is a [Kubernetes Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/) that completes the installation monitoring once all hosts have booted from disk, and agents are therefore no longer running.
1. Start the bootstrap services (`bootkube.service`, `approve-csr.service`, `progress.service`), at this point the bootstrap will start a temporary control plane.
1. Use the `kubeconfig-loopback` (part of the bootstrap ignition) and wait for 2 control plane nodes to appear.
1. Wait for the bootkube service to complete.
1. Execute the non-bootstrap installation flow.
1. Get approved by the `assisted-controller`.

The assisted-controller:

- Approves any node that tries to join the cluster (by approving the certificate sign requests)
- Lists the nodes in the cluster and reports installation progress.
- Monitors progress of operator installation, specifically console, CVO, and additional operators selected by the user (e.g., OCS, CNV).
- Collects logs and posts them to the service's REST API.
- Once all nodes have joined, notifies the installation has completed, and exits.
