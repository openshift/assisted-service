# Hive Integration

The goal of the Hive integration is to enable Assisted Installer capabilities on-premise in users' "Hub" clusters by installing clusters via Multi-cluster management, such as through [Hive](https://github.com/openshift/hive/) and [RHACM](https://github.com/open-cluster-management) (Red Hat Advanced Cluster Management).

A full description of the enhancement is available [here](https://github.com/openshift/enhancements/blob/master/enhancements/installer/agent-based-installation-in-hive.md).

For this integration, the Assisted Installer APIs are available via [CRDs](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)

## CRD Types

### [ClusterDeployment](https://github.com/openshift/hive/blob/master/apis/hive/v1/clusterdeployment_types.go)
The ClusterDeployment CRD is defined in the Hive repository, where additional fields are added for Agent Based installation.
In the ClusterDeployment, the user can specify requirements like Networking, number of Control Plane and Workers nodes and more.

The installation will start automatically if the required number of hosts is available, the hosts are ready to be installed and the Agents are approved.

### [InstallEnv](https://github.com/openshift/assisted-service/blob/master/internal/controller/api/v1beta1/installenv_types.go)
The InstallEnv CRD represents the configuration needed to create the discovery ISO.
The user can specify proxy settings, ignition overrides and specify NMState labels.

When the ISO is ready, an URL will be available in the CR.


### [NMStateConfig](https://github.com/openshift/assisted-service/blob/master/internal/controller/api/v1beta1/nmstate_config_types.go)
The NMStateConfig contains network configuration that will applied on the hosts. See NMState repository [here](https://github.com/nmstate/nmstate).


### [Agent](https://github.com/openshift/assisted-service/blob/master/internal/controller/api/v1beta1/agent_types.go)
The Agent CRD represents a Host that boot from an ISO and registered to a cluster.
It will be created by Assisted Service when a host registers.
In the Agent, the user can specify the hostname, role, installation disk and more.
Also, the host hardware inventory and statuses are available.

Note that if the Agent is not Approved, it will not be part of the installation.

Here how to approve an Agent:

```sh
$ kubectl -n assisted-installer patch agents.agent-install.openshift.io 120af504-d88e-46bd-bec2-b8b261db3b01 -p '{"spec":{"approved":true}}' --type merge
```

## Day 2 worker
Once the cluster is installed, the ClusterDeployment is set to Installed and secrets for kubeconfig and credentials are created and referenced in the ClusterDeployment.

In the Assisted Service, the original cluster is deleted and a Day 2 cluster is created instead.

Additional nodes can be added by booting from the new generated ISO. Each additional host will start installation once the Agent is Approved and the Host is in known state.

Note that the user needs to approved the additional nodes in the installed cluster.

## Bare Metal Operator Integration

In case that the Bare Metal Operator is installed, the Baremetal Agent Controller will sync between the Agent CR and the matching BareMetalHost CR:

- Find the right pairs of BMH/Agent using their MAC addresses
- Set the Image.URL in the BMH copying it from the InstallEnv's status.
- Reconcile the Agent's spec by copying the following attributes from the BMH's annotations:
    - Role: master/worker
    - Hostname (optional for user to set)
    - MachineConfigPool (optional for user to set)
- Reconcile the BareMetalHost hardware details by copying the Agent's inventory data to the BMH's `hardwaredetails` annotation.


See BMAC documentation [here](./baremetal-agent-controller.md).

## Assisted Installer Kube API CR examples

[docs/crds](https://github.com/openshift/assisted-service/tree/master/docs/crds) stores working examples of various resources we spawn via kube-api in assisted-installer, for Hive integration.
Those examples are here for reference.

You will likely need to adapt those for your own needs.

* [InstalllEnv](crds/installEnv.yaml)
* [NMState Config](crds/nmstate.yaml)
* [Hive PullSecret Secret](crds/pullsecret.yaml)
* [Hive ClusterDeployment](crds/clusterDeployment.yaml)
* [Hive ClusterDeployment-SNO](crds/clusterDeployment-SNO.yaml)



### Creating InstallConfig overrides

In order to alter the default install config yaml used when running `openshift-install create` commands.
More information about install-config overrides is available [here](user-guide/install-customization.md#Install-Config)
In case of failure to apply the overrides the clusterdeployment conditions will reflect the error and show the relevant error message. 

Add an annotation with the desired options, the clusterdeployment controller will update the install config yaml with the annotation value.
Note that this configuration must be applied prior to starting the installation
```sh
$ kubectl annotate clusterdeployments.hive.openshift.io test-cluster -n assisted-installer agent-install.openshift.io/install-config-overrides="{\"controlPlane\":{\"hyperthreading\":\"Disabled\"}}"
clusterdeployment.hive.openshift.io/test-cluster annotated
```

```sh
$ kubectl get clusterdeployments.hive.openshift.io test-cluster -n assisted-installer -o yaml
```
```yaml
apiVersion: hive.openshift.io/v1
kind: ClusterDeployment
metadata:
  annotations:
    agent-install.openshift.io/install-config-overrides: '{"controlPlane":{"hyperthreading":"Disabled"}}'
  creationTimestamp: "2021-04-01T07:04:49Z"
  generation: 1
  name: test-cluster
  namespace: assisted-installer
  resourceVersion: "183201"
  selfLink: /apis/hive.openshift.io/v1/namespaces/assisted-installer/clusterdeployments/test-cluster
  uid: 25769614-52db-448d-8366-05cb38c776fa
spec:
```

### Creating host installer args overrides

In order to alter the default coreos-installer arguments used when running `coreos-installer`openshift-install create command.
List of supported args can be found here https://github.com/openshift/assisted-service/blob/master/internal/host/hostutil/host_utils.go#L165
In case of failure to apply the overrides the agent conditions will reflect the error and show the relevant error message. 

Add an annotation with the desired options, the bmac controller will update the agent spec with the annotation value.
Then agent controller will forward it to host configuration.
Note that this configuration must be applied prior to starting the installation
```sh
$ kubectl annotate bmh openshift-worker-0 -n assisted-installer bmac.agent-install.openshift.io/installer-args="[\"--append-karg\", \"ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none\", \"--save-partindex\", \"1\", \"-n\"]"
baremetalhost.metal3.io/openshift-worker-0 annotated
```

```sh
$ oc get bmh openshift-worker-0 -n assisted-installer -o yaml
```
```yaml
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  annotations:
    bmac.agent-install.openshift.io/installer-args: '["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]'
  creationTimestamp: "2021-04-13T10:46:57Z"
  generation: 1
  name: openshift-worker-0
  namespace: assisted-installer
spec:
```