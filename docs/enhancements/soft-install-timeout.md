---
title: soft-install-timeout
authors:
- "@jhernand"
- "@oamizur"
creation-date: 2023-10-04
last-updated: 2023-11-23
---

# Soft install timeout

## Summary

Currently cluster installation by assisted installer may fail due to timeout expiration.
Assisted installer maintains many timeouts with different values.  A timeout limits the time
period to run an installation stage or to perform a specific installation operation. 
We want to change these timeouts to be soft.  Soft timeout expiration will cause warning that the
installation is taking longer than expected and the installation will continue.

## Motivation

This is important because users may be able to fix the issues that delay the
installation instead of having to start it over.

### Goals

### Non-Goals

It is not a goal to change the global installation timeout configured via the
`INSTALLATION_TIMEOUT` environment variable. That is set by default to `24h`
and at that point the assisted service stops monitoring the cluster.

## Proposal

### User Stories

#### Allow manually fixing a SaaS installation

As a user I tried to create a SNO cluster in the SaaS environment. An issue
prevented one of the cluster operators from reporting success. After one hour
the timeout expired and the installation was marked as failed. When I found the
failed install, I was able to easily resolve the issue and get the cluster
operator to report success. But the service no longer cared; as far as it was
concerned, the installation failed and was permanently marked as such. It also
would not give me the kubeadmin password, which is an important feature of the
install experience and tricky to obtain otherwise. I would like the service
inform me about the exceeded timeout, but it should give me the kubeadmin
password (if available at that point) and after my fixes it should continue
and eventually mark the installation as successful.

#### Allow manually fixing a late binding installation

As a user I tried to install a cluster with late binding featured enabled
(deleting the cluster will return the hosts to infrastructure environment). The
installation timed out and the installation was marked as failed. I connected
to the cluster and manually fixed the issue. The service will still think that
there is an error in the cluster, and if I try to perform day 2 operations on
it they will fail. The only option is to delete the cluster and create another
one that is marked as installed, but that will cause the host to boot from
discovery ISO. I would like the service to inform me of the exceeded timeout,
but after my fixes it should continue and eventually mark the installation as
successful.

### Implementation Details/Notes/Constraints

To be done.

### Risks and Mitigations

To be done.

## Design Details

### Existing timeouts and handling

There are several timeout types in assisted installer. Each timeout type is handled differently:


| Timeout                    | Entity  |                                   Type |          Managed by |                         Environment variable |                   Default |                                                        Action | Description |
|----------------------------|:-------:|---------------------------------------:|--------------------:|---------------------------------------------:|--------------------------:|--------------------------------------------------------------:|------------:|
| Prepare for installation   | cluster |                                 Status |    Assisted service |             PREPARE_FOR_INSTALLATION_TIMEOUT |                       10m |                                                 Move to ready |
| Installation               | cluster |                                 Status |    Assisted service |                         INSTALLATION_TIMEOUT |                       24h |                                                 Move to error |
| Finalizing                 | cluster |                                 Status |    Assisted service |                           FINALIZING_TIMEOUT |                        5h |                                                 Move to error |
| Installation in progress   | host |                        Stage (general) |    Assisted service |                                   Hard coded |                       60m |                                                 Move to error |
| Starting installation      | host |                                  stage |    Assisted service |     HOST_STAGE_STARTING_INSTALLATION_TIMEOUT |                       30m |                                                 Move to error |
| Installing                 | host |                                  stage |    Assisted service |                HOST_STAGE_INSTALLING_TIMEOUT |                       60m |                                                 Move to error |
| Waiting for control plane  | host |                                  stage |    Assisted service | HOST_STAGE_WAITING_FOR_CONTROL_PLANE_TIMEOUT |                       60m |                                                 Move to error |
| Waiting for controller     | host|                                  stage |    Assisted service |    HOST_STAGE_WAITING_FOR_CONTROLLER_TIMEOUT |                       60m |                                                 Move to error |
| Waiting for bootkube       | host|                                  stage |    Assisted service |      HOST_STAGE_WAITING_FOR_BOOTKUBE_TIMEOUT |                       60m |                                                 Move to error |
| Joined                     | host|                                  stage |    Assisted service |                    HOST_STAGE_JOINED_TIMEOUT |                       60m |                                                 Move to error |
| Writing image to disk      | host|                                  stage |    Assisted service |     HOST_STAGE_WRITING_IMAGE_TO_DISK_TIMEOUT |                       30m |                                                 Move to error |
| Configuring                | host|                                  stage |    Assisted service |               HOST_STAGE_CONFIGURING_TIMEOUT |                       60m |                                                 Move to error |
| Waiting for ignition       | host|                                  stage |    Assisted service |      HOST_STAGE_WAITING_FOR_IGNITION_TIMEOUT |                       24h |                                                 Move to error |
| Rebooting                  | host|                                  stage |    Assisted service |                 HOST_STAGE_REBOOTING_TIMEOUT |                       40m |                                      Move pending user action |
| Wait for nodes             | installation (cluster)| controller | Assisted controller | hard coded |                       10h |                                       Abort waiting for nodes |
| Wait for finalizing        | installation (cluster)| controller | Assisted controller | hard coded |                       10h | Don't perform post install + don't send complete installation |
| Wait for cluster operators | installation (cluster)| controller | Assisted controller | hard coded |                       10h |  Don't perform rest of controller operations | Only CVO and console |
| Add router CA              | installation (cluster)| controller | Assisted controller | hard coded |                       70m | Don't perform rest of controller operations |
| Wait for OLM operators     | installation (cluster)| controller | Assisted controller || calculated from operators | | Don't perform rest of controller operations |
| Apply manifests            | installation (cluster)| controller | Assisted controller | hard coded |                       10m |  Don't perform rest of controller operations |
| Wait for OLM operators CSV | installation (cluster)| controller | Assisted controller | | calculated from operators | Don't perform rest of controller operations |
| Send complete installation | installation (cluster)| controller | Assisted controller | hard coded | 30m |  Don't perform rest of controller operations |


There are 2 flows for completing installation.  One is managed by controller, and one by assisted service.

In the flow managed by the assisted service, the following steps must be completed:

- kubeconfig must be uploaded
- cluster operators are successful
- Monitored operators are either successful or failed

In the flow managed by the controller all steps must be completed (in the table above).  The last step is a 
a notification to complete installation.

### Suggested changes
- Only assisted service should notify installation completion
- All steps by controller will be known as stages in the assisted service.  This will enable the service to manage 
them in a similar way that host stages are managed.
- Controller will not stop activity due to timeout.
- Assisted service should be able to terminate the controller.
- All existing timeouts except installation timeout and rebooting timeout should be treated as soft timeouts (i.e notification only)
- In case of some optional operation (i.e managed operators), the user will be able to decide to skip it and set the cluster as completed.
- The suggested functionality should be optional. For SaaS it will be enabled by default and will be disabled by default
for ZTP.

### Open Questions

- Do we also soften the 24h `INSTALLATION_TIMEOUT` timeout?

- Should we collect must gather logs when the installation timeout expires, even
  if we don't mark it as failed?

### UI Impact

The UI will need to explicitly show to the user that the cluster installation
is taking longer than expected, and give suggestions on how to proceed. For
example, we could present a warning message with this text:

> Cluster installation is taking too long
>
> Most installations complete in approximately 45 minutes, but it took 23 hours
> and 52 minutes already. Check the logs to find out why or reset the cluster
> to start over.

The progress bars and other UI elements used to indicate progress should also
explicitly indicate that the installation is taking longer than expected, for
example using warning icons or specific colors. For example:

![UI example](./soft-install-timeout/ui-example.png)

### Test Plan

We will need the following test cases, for the preparation, installation and
finalizing phases:

- Prepare a cluster that exceeds the timeout and verify that fixing the issue
  manually allows the service to continue with the installation.

- Verify that the UI explicitly shows the information about the expired
  timeout, and that it recovers when the issue is eventually fixed and the
  service continues with the installation.

These tests may require introducing a mechanism to artificially delay the
installation in the agent or the installer.

## Drawbacks

Most of our cluster installation failures are due to these timeouts. If we just
disable them then we will have a large amount of installations that have failed
but will not be accounted as such.

## Alternatives

None.
