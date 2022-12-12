---
title: agent-upgrade
authors:
  - "@jhernand"
creation-date: 2022-06-13
last-updated: 2022-06-13
---

# Agent upgrade

## Summary

Usually the time since the agent is started and the cluster is installed is
short. But in some situations it may be long, even many days, and during that
long time the service may be upgraded and may require a newer version of the
agent. There is currently no mechanism to upgrade the agent, the service
silently works with the older agent assuming that nothing will go wrong. The
only way to upgrade the agent would be to reboot the host, and we don't ask
users to do that. Even if we did, it would be inconvenient, specially if there
are many hosts or if rebooting a host requires manual intervention. This is
particularly common in the late binding scenario. This is a proposal to
implement a mechanism to upgrade the agent without having to reboot the host.

Note that all new agent versions require a rollout of the service pods (as the
agent image name is delivered as an environment variable to the service). Not
all rollouts of service pods come with a newer agent version.

## Motivation

Reduce compatibility concerns when designing and developing new features.

Reduce the need to reboot hosts when there are changes in the service that
require new versions of the agent.

### Goals

The agent image will be automatically upgraded to the version expected by the
service before proceeding to the installation of the cluster.

Note that in this context _version_ is really a container image name, like
`registry-proxy.engineering.redhat.com/rh-osbs/openshift4-assisted-installer-agent-rhel8:v1.0.0-141`.

### Non-Goals

The upgrade feature will not work for changes in the part of the agent that
performs the initial registration since the upgrade part relies on the
registration mechanism.

Upgrading the agent after installation has started is out of scope for this
enhancement.

Upgrading other images like the image of the service itself of the image of the
assisted installer or installer controller is completely out of the scope of
this enhancement.

## Proposal

### User Stories

#### Story 1

As a developer I want the agent to be automatically upgraded to what the service
expects, so that I can assume that the service will never proceed to install a
cluster with a host that is running an incompatible version of the agent.

### Implementation Details/Notes/Constraints

A new `compatible-agent` validation will be added to the service to check if
the agent is compatible.

The logic for that validation will be a comparison of the value of the
`AGENT_DOCKER_IMAGE` environment variable passed to the service, and the
`discovery_agent_version` header sent by the agent and stored in the `hosts`
table.

The `discovery_agent_version` currently reported by the agent is only the image
tag. For example, if the full image reference is
`registry-proxy.engineering.redhat.com/rh-osbs/openshift4-assisted-installer-agent-rhel8:v1.0.0-141`
the the agent will send only `v1.0.0-141` in the `discovery_agent_version`
header. That will need to be changed in the agent, so that it sends the full
image reference. The server will be changed to extract from that full image
reference the tag where required, in particular for generation of metrics.

In order to support both old and new agents the validation will not be a simple
string comparison. Instead it will check if the `discovery_agent_version` sent
by the agent is a full image reference or only a tag. If it is a full image
reference then it will be a exact string comparison with the value of the
`AGENT_DOCKER_IMAGE` environment variable. If it is only a tag, then it will be
a comparison with the tag extracted from the value of the `AGENT_DOCKER_IMAGE`
environment variable. In this last case a warning will be written to the log
explaining that the comparison may be unreliable because only the tags were
compared.

To make this comparison useful it is important that the `AGENT_DOCKER_IMAGE`
environment variable passed to the service doesn't use the `:latest` tag, or in
general any moving tag, as that renders the comparison useless. In the _SaaS_
stage environment that variable is already using a tag, so that shouldn't be a
problem:

```
$ oc get deployment assisted-service -n assisted-installer-stage -o json | \
jq '.spec.template.spec.containers[].env[] | select(.name == "AGENT_DOCKER_IMAGE")'
{
  "name": "AGENT_DOCKER_IMAGE",
  "value": "registry-proxy.engineering.redhat.com/rh-osbs/openshift4-assisted-installer-agent-rhel8:v1.0.0-141"
}
```

In the integration and development environments we are using the `:latest` tag,
which effectively disables this feature.

The service will check during startup the value of the `AGENT_DOCKER_IMAGE` and
it will write a warning to the log if it is using the `:latest` tag.

A new `upgrade-agent` step will be added to the service and to the agent.

When the service receives a request for next steps from a host it will check if
that host is in one of the following states:

- `binding`
- `disabled-unbound`
- `disabled`
- `disconnected-unbound`
- `disconnected`
- `discovering-unbound`
- `discovering`
- `insufficient-unbound`
- `insufficient`
- `known-unbound`
- `known`
- `pending-for-input`
- `unbinding-pending-user-action`
- `unbinding`

If the host is in one of the above states then the service will check the result
of the `compatible-agent` validation. If the validation passed then the service
will return the usual next steps. If the validation didn't pass then the service
will return only the `upgrade-agent` step, replacing all the usual steps.

If on the other hand the host is in one of the following states:

- `added-to-existing-cluster`
- `cancelled`
- `error`
- `installed`
- `installing-in-progress`
- `installing`,
- `preparing-failed`
- `preparing-for-installation`
- `preparing-successful`
- `resetting-pending-user-action`
- `resetting`

Then the service will return the usual next steps regardless of the result of
the validation. The reason for this is that we don't want to interrupt ongoing
installations or log gathering.

The `upgrade-agent` request will have a `agent_image` field containing the full
reference of the image that the agent should upgrade to.

The `upgrade-agent` response will have a `agent_image` field containing the
full reference of the image that the agent has upgraded to, and a `result`
field to indicate if downloaded the image succeeded or failed.

The service will generate a `upgrade-agent-started` event when it sends the
`upgrade-agent` command to the agent.

The service will generate a `upgrade-agent-finished` event when the agent
successfully downloads the image, and a `upgrade-agent-failed` event when it
fails.

If the agent fails to download the image the service will keep sending it the
`upgrade-agent` command till it succeeds.

The agent will be changed so that it sends the full image reference in the
`discovery_agent_version` header.

When the agent receives the `upgrade-agent` step it will do the following:

1. It will check if the new image is already downloaded with the `podman image exists ...` command. If the image has already been downloaded it will return
   to the service a response indicating that the image has been successfully
   downloaded.

2. If the image hasn't been downloaded it will try to download it using `podman pull ...`. If that succeeds it will exit the next step runner process, so
   that the main process will start it again with the new image.

3. If downloading the image fails the agent will return to the service a
   response indicating it and will not exit. This means that the next step
   runner will continue to run, and it will eventually receive again from the
   service the instruction to upgrade, and therefore it will try again to download
   the image.

The `upgrade-agent` step is also (similar to register) something we must not
break in order to allow agent upgrade.

### Risks and Mitigations

The main risk is introducing interference with the state machine that controls
the hosts. To reduce the chances of that the upgrade will be performed only when
the host is not in the process of installing.

The feature will also be behind a feature flag controlled by the
`ENABLE_UPGRADE_AGENT` environment variable.

## Design Details

None, see the implementation details above.

### Open Questions

- Do the _on premise_ installations use values of `AGENT_DOCKER_IMAGE` with the
  `:latest` tag? If so that will need to be changed to use a unique version tag or
  a SHA, otherwise the feature will be effectively disabled.

- Do we want an different handling for the new `compatible-agent` validation in
  the UI?

- Should we add a new `upgrade_status` text field to the host type to store the
  current status of the upgrade? This would be with information sent by the
  agent, something like "downloading new agent image", "failed to download new
  agent image", etc. It could be useful to have this in the Kubernetes API
  because events aren't available for users there.

### UI Impact

The UI will at the very minimum need to display correctly the new
`compatible-agent` validation. Without changes it will display the raw
`compatible-agent` string as the name of the validation. We will probably want
to change that to display a more user friendly name, like "Agent compatibility".

We will probably want to change the way that the result of this validation is
presented to the user. Without change it will be presented like any other failed
validation, and that means that the user will assume that she has to perform
some corrective action. In this case the corrective action is automatic, and
only explained in the description of the validation.

The description of the `compatible-agent` validation will be the following:

> The installation cannot start just yet because this host's agent is in the
> process of being upgraded to a newer version, please wait, this could take
> a few minutes.

When the validation fails it will be presented with an hourglass icon, or
something else less frightening than the regular error icon.

### Test Plan

We will need the following test cases for the service:

- Send an initial registration request to the service containing an incompatible
  agent image and verify that the validation fails.

- Send an initial registration request to the service containing a compatible
  agent image and verify that the validation passes.

- Send an initial registration request with a compatible agent image, then send
  a request for next steps with an incompatible agent and verify that the
  validation fails.

- Send an initial registration request with a compatible agent image, then send
  a request for next steps with the same compatible agent image and verify that
  the validation passes.

- Send an initial registration request with a compatible agent image. Move the
  host to the `preparing-for-install`, `installing` and `installing-in-progress`
  states. For each of those states send a request for next steps with an
  incompatible agent image and verify that the new `upgrade-agent` step is not
  returned.

- Send an initial registration request with a compatible agent image. Move the
  host to any non-installing state. For each of those states send a request for
  next steps with an incompatible agent image and verify that the new
  `upgrade-agent` step is returned.

We will need the following test cases for the agent:

- Send a request for next steps, make sure that the result is the
  `upgrade-agent` step and verify that the agent is restarted with the new agent
  image.

We will need the following end to end test cases:

- Start the service as usual, with the default `AGENT_DOCKER_IMAGE` environment
  variable. Register a host. Restart the service with a different value of the
  `AGENT_DOCKER_IMAGE`. Doesn't need to be a different image, just a different
  value, for example the SHA of that image. Verify that the agent service is
  restarted and registers again with the new image name.

## Drawbacks

Using a validation for this feature may be confusing for users if we don't
change the UI to clearly indicate that when an agent isn't compatible with the
service there is no need for manual corrective actions.

## Alternatives

Introduce a new `upgrading` state in the agent state machine.
