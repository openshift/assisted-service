# Late Binding

With late binding, a discovery ISO can be created without the need of a reference to a Cluster Deployment.
Hosts booted with such an ISO can be bound to different clusters at a later stage.

See full enhancement documents here: [late binding](../enhancements/agent-late-binding.md) and [returning agents to InfraEnv](../enhancements/agents-back-to-infraenv.md).

## High Level Flow
- The user creates an InfraEnv CR without a Cluster Reference. See example [here](crds/infraEnvLateBinding.yaml)
- The version of the base RHCOS live ISO is selected automatically to the latest available.
- The Agent CR created from an host booted from this ISO will not have a Cluster Deployment reference set in it. (`spec.clusterDeploymentName`)
- The user creates a Cluster Deployment and Agent Cluster Install CR.
- The user updates the Agent's Cluster Deployment reference to the CD name:
```bash
kubectl -n my_namespace patch agents.agent-install.openshift.io my_agent -p '{"spec":{"clusterDeploymentName":{"name":"my_cd","namespace":"my_cd_ns"}}}' --type merge
```
- The process of the agent binding can be followed on the `Bound` condition available on the Agent CR. See [here]
(kube-api-conditions.md#agent-conditions)
- Once the agent is bound, the flow for installation is as before.

An agent can be unbound from a Cluster Deployment as long as the installation did not start.

If an agent is unbound after it was installed or if it is in `error`/`canceled` state, the Agent's `Bound` condition will be `False` with `UnbindingPendingUserAction` reason. In this state, it is the responsibility of the user to reboot the host with the discovery ISO.

With BareMetalOperator integration, the host will be rebooted automatically.

Note that the Pull Secret of the InfraEnv can be different from the one specified in the Cluster Deployment.

Once the host is rebooted into the discovery image (whether through the BareMetalOperator or manually) the related resources (Node, Machine, BMH, etc) will be removed assuming the cluster API is still accessible.
This process can be skipped or aborted by setting the annotation `agent.agent-install.openshift.io/skip-spoke-cleanup=true` on the Agent resource.


## Add IgnitionToken reference
In order for the agent to be able to pull the ignition, it need a reference to a token that will allow it to do so.
The token is reference using the "ignitionEndpointTokenReference" field in the agent spec.
It can be updated using:
```bash
kubectl -n my_namespace patch agents.agent-install.openshift.io my_agent -p '{"spec":{"ignitionEndpointTokenReference":{"name":"token_secret_name","namespace":"token_secret_namespace"}}}' --type merge
```

## Teardown

When a Cluster/Deployment is deleted, the Agents created with late binding will be returned to the InfraEnv.

When an InfraEnv CR is deleted, the hosts related to it will be deleted if they are Unbound or Installed.
If no more hosts are connected, the InfraEnv will be deleted.
If there are still hosts connected, the InfraEnv CR will not be deleted until all the related hosts are deleted or Unbound.

## Unsupported flows

The following operations are not supported:

- Update/Add/Delete a Cluster Deployment reference of an InfraEnv.
- Update a Cluster Deployment reference of an Agent after installation started.
