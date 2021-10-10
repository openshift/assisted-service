# Late Binding

With late binding, a discovery ISO can be created without the need of a reference to a Cluster Deployment.
Hosts booted with such an ISO can be bound to different clusters at a later stage.

See full enhancement document [here](../enhancements/agent-late-binding.md).

## High Level Flow
- The user creates an InfraEnv CR without a Cluster Reference. See example [here](crds/infraEnvLateBinding.yaml)
- The version of the base RHCOS live ISO is selected automatically to the latest available.
- The Agent CR created from an host booted from this ISO will not have a Cluster Deployment reference set in it. (`spec.clusterDeploymentName`)
- The user creates a Cluster Deployment and Agent Cluster Install CR.
- The user updates the Agent's Cluster Deployment reference to the CD name:
```bash
kubectl -n assisted-installer patch agents.agent-install.openshift.io 039d72c2-f15a-4a67-977e-b25fd1dfea07 -p '{"spec":{"clusterDeploymentName":{"name":"single-node","namespace":"assisted-installer"}}}' --type merge
```
- The process of the agent binding can be followed on the `Bound` condition available on the Agent CR. See [here]
(kube-api-conditions.md#agent-conditions)
- Once the agent is bound, the flow for installation is as before.

An agent can be unbound from a Cluster Deployment as long as the installation did not start.

Note that the Pull Secret of the InfraEnv can be different from the one specified in the Cluster Deployment.

## Unsupported flows

The following operations are not supported:

- Update/Add/Delete a Cluster Deployment reference of an InfraEnv.
- Update a Cluster Deployment reference of an Agent after installation started.