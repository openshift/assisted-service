Baremetal Agent Controller (a.k.a BMAC)
==

BMAC is a Kubernetes controller responsible for reconciling [BareMetalHost][bmo] and Agent (defined
and maintained in this repo) resources for the agent-based deployment scenario.

Testing
==

The testing environment for BMAC consists of

- [Downstream dev-scripts][dev-scripts] deployment
- [Baremetal Operator][bmo]: It defines the BareMetalHost custom resource
- [Assisted Installer Operator](../operator.md): To deploy and manage the  assisted installer
  deployment. Read the operator docs to know more about its dependencies and installation process.


Each of the components listed above provide their own documentation on how to deploy and configure
them. However, you can find below a set of recommended configs that can be used for each of these
components:

Dev Scripts
===

```bash
# Giving Master nodes some extra CPU since we won't be
# deploying any workers
export MASTER_VCPU=4
export MASTER_MEMORY=20000

# Set specs for workers
export WORKER_VCPU=4
export WORKER_MEMORY=20000
export WORKER_DISK=60

# No workers are needed to test BMAC
export NUM_WORKERS=0

# Add extra workers so we can use it for the deployment.
# SNO requires 1 extra machine to be created.
export NUM_EXTRA_WORKERS=1

# At the time of this writing, this requires the 1195 PR
# mentioned below.
export PROVISIONING_NETWORK_PROFILE=Disabled

# Add extradisks to VMs
export VM_EXTRADISKS=true
export VM_EXTRADISKS_LIST="vda vdb"
export VM_EXTRADISKS_SIZE="30G"

export REDFISH_EMULATOR_IGNORE_BOOT_DEVICE=True
```

The config above should provide you with an environment that is ready to be used for the operator,
assisted installer, and BMAC tests. Here are a few tips that would help simplifying the environment
and the steps required:

- Clone [baremetal-operator][bmo] somewhere and set the `BAREMETAL_OPERATOR_LOCAL_IMAGE` in your config.

**NOTE**

The default hardware requirements for the OCP cluster are higher than the values provided below. A guide on how to customize validator requirements can be found [here](../dev/hardware-requirements.md).

Local Baremetal Operator (optional)
==

**NOTE**

This section is completely optional. If you don't need to run your own clone of the
[baremetal-operator][bmo], just ignore it and proceed to the next step.

---

The [baremetal-operator][bmo] will define the BareMetalHost custom resource required by the agent
based install process. Setting the `BAREMETAL_OPERATOR_LOCAL_IMAGE` should build and run the BMO
already. However, it's recommended to run the [local-bmo][local-bmo] script to facilitate the
deployment and monitoring of the BMO. Here's what using [local-bmo][local-bmo] looks like:

It's possible to disable inspection for the master (and workers) nodes before running the local-bmo
script. This will make the script less noisy which will make debugging easier.

```bash
./metal3-dev/pause-control-plane.sh
```
The `pause-control-plane.sh` script only pauses the control plane. You can do the same for the worker
nodes with the following command

```bash
for host in $(oc get baremetalhost -n openshift-machine-api -o name | grep -e '-worker-'); do
    oc annotate --overwrite -n openshift-machine-api "$host" \
       'baremetalhost.metal3.io/paused=""'
done
```

The steps mentioned above are optional, and only recommended for debugging purposes. Let's now run
[local-bmo][local-bmo] and move on. This script will tail the logs so do it in a separate buffer so
that it can be kept running.

```bash
# Note variable is different from the one in your dev-script
# config file. You can set it to the same path, though.
export BAREMETAL_OPERATOR_PATH=/path/to/your/local/clone
./metal3-dev/local-bmo.sh
```

Assisted Installer Operator
===

Once the dev-script environment is up-and-running, and the [bmo][bmo] has been deployed, you can
proceed to deploying the Assisted Installer Operator. There's a script in the dev-scripts repo that
facilitates this step:

```
[dev@edge-10 dev-scripts]$ ./assisted_deployment.sh install_assisted_service
```

Take a look at the [script itself][assisted-deployment-sh]
to know what variables can be customized for the Assisted Installer Operator deployment.

Creating AgentClusterInstall, ClusterDeployment and InfraEnv resources
===

A number of resources has to be created in order to have the deployment fully ready for deploying OCP clusters. A typical workflow is as follows

* create the [PullSecret](crds/pullsecret.yaml)
  * in order to create it directly from file you can use the following
  ```
  kubectl create secret -n assisted-installer generic pull-secret --from-file=.dockerconfigjson=pull_secret.json
  ```
* create the [ClusterImageSet](crds/clusterImageSet.yaml)
* optionally create a [custom `ConfigMap` overriding default Assisted Service configuration](operator.md#specifying-environmental-variables-via-configmap)
* create the [AgentClusterInstall](crds/agentClusterInstall.yaml) or [AgentClusterInstall for SNO](crds/agentClusterInstall-SNO.yaml)
  * more manifests (e.g. IPv6 deployments) can be found [here](https://docs.google.com/document/d/1jDrwSyKFssIh-yxJ-wSdB-OCcPvsfm06P54oTk1C6BI/edit#heading=h.acv4csx2xph6)
* create the [ClusterDeployment](crds/clusterDeployment.yaml)
* create the [InfraEnv](crds/infraEnv.yaml)
* patch BareMetalOperator to watch namespaces other than `openshift-machine-api`
  ```
  $ oc patch provisioning provisioning-configuration --type merge -p '{"spec":{"watchAllNamespaces": true}}'
  ```

---
**NOTE**

When deploying `AgentClusterInstall` for SNO it is important to make sure that `machineNetwork` subnet matches the subnet used by libvirt VMs (configured by passing `EXTERNAL_SUBNET_V4` to the [dev-scripts config](https://github.com/openshift-metal3/dev-scripts/blob/master/config_example.sh)). It defaults to `192.168.111.0/24` therefore the sample manifest linked above needs to be adapted.

At this moment it's good to check logs and verify that there are no conflicting parameters, the ISO has been created correctly and that the installation can be started once a suitable node is provided.

To check if the ISO has been created correctly, do
```
oc get infraenv myinfraenv -o jsonpath='{.status.isoDownloadURL}' -n assisted-installer
```

Creating BareMetalHost resources
===

The [baremetal operator][bmo] creates the `BareMetalHost` resources for the existing nodes
automatically. For scenarios using extra worker nodes (like SNO), it will be necessary to create
`BareMetalHost` resources manually. Luckily enough, `assisted_deployment.sh` is one step ahead
and it has prepared the manifest for us already.

```
less ocp/ostest/saved-assets/assisted-installer-manifests/06-extra-host-manifests.yaml
```

The created `BareMetalHost` manifest contains already a correct namespace as well as annotations to disable the inspection and cleaning. Below is an example on what it could look like.

Please remember to change the value of the `infraenvs.agent-install.openshift.io` label in case you are using different than the default one (`myinfraenv`).

```
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: ostest-worker-0
  namespace: assisted-installer
  annotations:
    inspect.metal3.io: disabled
  labels:
    infraenvs.agent-install.openshift.io: "myinfraenv"
spec:
  online: true
  bootMACAddress: 00:ec:ee:f8:5a:ba
  automatedCleaningMode: disabled
  bmc:
    address: ....
    credentialsName: bmc-secret
```

Setting `inspect.metal3.io` is optional since BMAC will add it automatically. 
Without it, the `BareMetalHost` will boot IPA and spend additional time in the
inspecting phase when the manifest is applied.

Setting the `infraenvs.agent-install.openshift.io` is required and it must be set to the name of
the InfraEnv to use. Without it, BMAC won't be able to set the ISO Url in the BareMetalHost resource.

It is possible to specify `RootDeviceHints` for the `BareMetalHost` resource. Root device hints are
used to tell the installer what disk to use as the installation disk. Refer to the
[baremetal-operator documentation](https://github.com/metal3-io/baremetal-operator/blob/master/docs/api.md#rootdevicehints) to know more.

---
**NOTE**

Prior to ACM 2.9: BMAC sets the field `automatedCleaningMode` to `disabled` even if 
the `BareMetalHost` manifest specifies another value (e.g. `automatedCleaningMode: metadata`).

In ACM 2.9+: BMAC no longer sets `automatedCleaningMode` and will respect the value set
in the `BareMetalHost` manifest when created.

If the `automatedCleaningMode` field is not set when the `BareMetalHost` manifest is created, then the 
BMO's webhook will automatically set the field to `metadata`.

---
**NOTE**

BMAC uses the `RootDeviceHints` from the `BareMetalHost` resource to find a matching disk in the corresponding `Agent`'s inventory, and then sets that disk in `agent.Spec.InstallationDiskID`. If the hints provided to the BMH do not match any disk discovered in the discovery phase, BMAC sets the disk in the Agent's Spec as `/dev/not-found-by-hints`. This will cause the service to notice that the specified disk does not exist and fail the respective validation, showing the following condition in the `Agent` resource.

```
message: 'The Spec could not be synced due to an input error: Requested installation
  disk is not part of the host''s valid disks'
reason: InputError
```

The log of the service will show the following message indicating that the specified disk (as set by BMAC) is not available. Please note this message is not exposed to the user, but only written in the service log:

```
level=error msg="failed to set installation disk path </dev/not-found-by-hints> host <29d87175-[...]-eb1efd15fdc0> infra env <d46b1dcd-[...]-2412cd69a0b4>" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).updateHostDisksSelectionConfig" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:5229" error="Requested installation disk is not part of the host's valid disks"
```

Installation flow
===

After all the resources described above are created the installation starts automatically. A detailed flow is out of scope of this document and can be found [here](architecture.md#installation-flow).

An `Agent` resource will be created that can be monitored during the installation proces as in the example below

```
$ oc get agent -A
$ oc get agentclusterinstalls test-agent-cluster-install -o json | jq '.status.conditions[] |select(.type | contains("Completed"))'
```

After the installation succeeds there are two new secrets created in the `assisted-installer` namespace

```
assisted-installer  single-node-admin-kubeconfig  Opaque  1  12h
assisted-installer  single-node-admin-password    Opaque  2  12h
```

Kubeconfig can be exported to the file with

```
$ oc get secret single-node-admin-kubeconfig -o json -n assisted-installer | jq '.data' | cut -d '"' -f 4 | tr -d '{}' | base64 --decode > /tmp/kubeconfig-sno.yml
```

---
**NOTE**

`ClusterDeployment` resource defines `baseDomain` for the installed OCP cluster. This one will be used in the generated kubeconfig file so it may happen (depending on the domain chosen) that there is no connectivity caused by name not being resolved. In such a scenario a manual intervention may be needed (e.g. manual entry in `/etc/hosts`).

Troubleshooting
==

- I have created the BMH, the ClusterDeployment, and the InfraEnv resources. Why doesn't the node start?

The first thing to do is to verify that an ISO has been created and that it is associated with the
BMH. Here are a few commands that can be run to achieve this:

```
$ oc describe infraenv $YOUR_INFRAENV | grep ISO
$ oc describe bmh $YOUR_BMH | grep Image
```
**NOTE**

In case the HUB cluster version is 4.11 or above instead of checking the `Image` on the BMH you should check the PreprovisioningImage status:
```
$ oc get preprovisioningimage -n {BMH_NAMESPACE} {BMH_NAME} -ojsonpath={'.status}' | jq
```

---
- InfraEnv's ISO Url doesn't have an URL set

This means something may have gone wrong during the ISO generation. Check the assisted-service logs
(and docs) to know what happened.

---
- InfraEnv has an URL associated but the BMH Image URL field is not set:

Check that the `infraenvs.agent-install.openshift.io` label is set in your `BareMetalHost` resource
and that the value matches the name of the InfraEnv's. Remember that both resources **must** be in
the same namespace.

Check that resources in the `openshift-machine-api` are up and running. `cluster-baremetal-operator`
is responsible for handling the state of the BMH so if that one is not running, your BMH will never
move forward.

Check that `cluster-baremetal-operator` is not configured to ignore any namespaces or CRDs. You can
do it by checking the `overrides` section in

```
$ oc describe clusterversion version --namespace openshift-cluster-version
```

---
- URL is set everywhere, node still doesn't start

Double check that the `BareMetalHost` definition has `online` set to true. BMAC should take care of
this during the reconcile but, you know, software, computer gnomes, and dark magic.

---
- Node boots but it loooks like it is booting something else

Check that the `inspect.metal3.io` and `automatedCleaningMode` are both set to `disabled`. This will
prevent Ironic from doing inspection and any cleaning, which will speed up the deployment process
and prevent it from running IPA before running the ISO.

This should be set automatically by BMAC in the part linked [here](https://github.com/openshift/assisted-service/blob/v1.0.22.1/internal/controller/controllers/bmh_agent_controller.go#L531-L545)
but if that is not the case, start from checking the assisted-service logs as there may be more
errors related to the BMH.

**NOTE**

In case the HUB cluster version is 4.11 or above these annotations shouldn't be set and the IPA is expected to run on the node and register with Ironic.
Try checking the BMH custom deploy method is set to `start_assisted_install`
```
$ oc get bmh -n  {BMH_NAMESPACE} {BMH_NAME} -ojsonpath={'.spec.customDeploy}' | jq
```

---
- Node boots, but nothing else seems to be happening

Check that an agent has been registered for this cluster and BMH. You can verify this by chekcing
the existing agents and find the one that has an interface with a MacAddress that matches the BMH
`BootMACAddress`.

Remember that in between the node booting from the Discovery ISO and the Agent CR being created you
may need to wait a few minutes.

**NOTE**

In case the HUB cluster version is 4.11 or above, IPA is expected to start the assisted agent on the node in order for the node to register.
Try checking the BMH custom deploy method is set to `start_assisted_install`
```
$ oc get bmh -n  {BMH_NAMESPACE} {BMH_NAME} -ojsonpath={'.spec.customDeploy}' | jq
```


If there is an agent, the next thing to check is that all validations have passed. This can be done
by inspecting the `ClusterDeployment` and verify that the validation phase has succeeded.


[bmo]: https://github.com/openshift/baremetal-operator
[local-bmo]: https://github.com/openshift-metal3/dev-scripts/blob/master/metal3-dev/local-bmo.sh
[dev-scripts]: https://github.com/openshift-metal3/dev-scripts/
[assisted-deployment-sh]: https://github.com/openshift-metal3/dev-scripts/blob/master/assisted_deployment.sh
