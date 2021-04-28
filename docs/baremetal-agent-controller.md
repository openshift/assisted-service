Baremetal Agent Controller (a.k.a BMAC)
==

BMAC is a Kubernetes controller responsible for reconciling [BareMetalHost][bmo] and Agent (defined
and maintained in this repo) resources for the agent-based deployment scenario.

Testing
==

The testing environment for BMAC consists in:

- [Downstream dev-scripts](https://github.com/openshift-metal3/dev-scripts/) deployment
- [Baremetal Operator][bmo]: It defines the BareMetalHost custom resource
- [Assisted Installer Operator](./operator.md): To deploy and manage the  assisted installer
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

# Set memory and CPU for workers
export WORKER_VCPU=4
export WORKER_MEMORY=20000

# No workers are needed to test BMAC
export NUM_WORKERS=0

# Add extra workers so we can use it for the deployment
export NUM_EXTRA_WORKERS=1

# At the time of this writing, this requires the 1195 PR
# mentioned below.
export PROVISIONING_NETWORK_PROFILE=Disabled
```

The config above should provide you with an environment that is ready to be used for the operator,
assisted installer, and BMAC tests. Here are a few tips that would help simplifying the environment
and the steps required:

- Clone [baremetal-operator][bmo] somewhere and set the BAREMETAL_OPERATOR_LOCAL_IMAGE in your config.

Once dev-script is up and running, modify the worker(s) and add 2 more disks (10GB should be enough)
as they are required by the Assisted Installer Operator.

Local Baremetal Operator (optional)
==

The [baremetal-operator][bmo] will define the BareMetalHost custom resource required by the agent
based install process. Setting the `BAREMETAL_OPERATOR_LOCAL_IMAGE` should build and run the BMO
already. However, it's recommended to run the [local-bmo][local-bmo] script to facilitated the
deployment and monitoring of the BMO. Here's what using [local-bmo][local-bmo] looks like:

It's possible to disable inspection for the master (and workers) nodes before running the local-bmo
script. This will make the script less noisy which will make debugging easier.

```bash
./metal3-dev/pause-control-plane.sh
```
The pause-control-plane script only pauses the control plane. You can do the same for the worker
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
[dev@edge-10 dev-scripts]$ make assisted_deployment
```

Take a look at the [script itself](https://github.com/openshift-metal3/dev-scripts/blob/master/assisted_deployment.sh)
to know what variables can be customized for the Assisted Installer Operator deployment.

Creating BareMetalHost resources
===

The [baremetal operator][bmo] creates the `BareMetalHost` resources for the existing nodes
automatically. For scenarios using extra worker nodes (like SNO), it will be necessary to create
`BareMetalHost` resources manually. Luckily enough, `dev-scripts` is one step ahead and it has
prepared the manifest for us already.

```
less ocp/ostest/extra_host_manifests.yaml
```

You can modify this manifest to disable inspection, and cleaning. Here's an example on what it would look like:

```
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: ostest-worker-0
  namespace: assisted-installer
  annotations:
    # BMAC will add this annotation if not present
    inspect.metal3.io: disabled
  labels:
    infraenvs.agent-install.openshift.io: "bmac-test"
spec:
  online: true
  bootMACAddress: 00:ec:ee:f8:5a:ba
  automatedCleaningMode: disabled
  bmc:
    address: ....
    credentialsName: bmc-secret
```

Setting `automatedCleaningMode` field and the `inspect.metal3.io` annotations are both optional. If
skipped, the `BareMetalHost` will boot IPA and spend some time in the inspecting phase when the
manifest is applied. Setting the `infraenvs.agent-install.openshift.io` is required and it must be
set to the name of the InfraEnv to use. Without it, BMAC won't be able to set the ISO Url in the
BareMetalHost resource.

It is possible to specify `RootDeviceHints` for the `BareMetalHost` resource. Root device hints are
used to tell the installer what disk to use as the installation disk. Refer to the
[baremetal-operator documentation](https://github.com/metal3-io/baremetal-operator/blob/master/docs/api.md#rootdevicehints) to know more.


[bmo]: https://github.com/openshift/baremetal-operator
[local-bmo]: https://github.com/openshift-metal3/dev-scripts/blob/master/metal3-dev/local-bmo.sh
[aspi-custom]: https://github.com/openshift/assisted-service/blob/master/config/default/assisted-service-patch-image.yaml


Creating ClusterDeployment and InfraEnv
==

Before deploying the ClusterDeployment, make sure you have created a secret with your pull-secret.
In this environment it's called `my-pull-secret`

```
kubectl create secret -n assisted-installer generic my-pull-secret --from-file=.dockerconfigjson=pull_secret.json
```

At this point, it's possible to deploy the SNO cluster by modifying and applying [the SNO manifest](./crds/clusterDeployment-SNO.yaml)

Troubleshooting
==

- I have created the BMH, the ClusterDeployment, and the InfraEnv resources. Why doesn't the node start?

The first thing to do is to verify that an ISO has been created and that it is associated with the
BMH. Here are a few commands that can be run to achieve this:

```
$ oc describe infraenv $YOUR_INFRAENV | grep ISO
$ oc describe bmh $YOUR_BMH | grep Image
```

- InfraEnv's ISO Url doesn't have an URL set

This means something may have gone wrong during the ISO generation. Check the assisted-service logs
(and docs) to know what happened.

- InfraEnv has an URL associated but the BMH Image URL field is not set:

Check that the `infraenvs.agent-install.openshift.io` label is set in your `BareMetalHost` resource
and that the value matches the name of the InfraEnv's. Remember that both resources **must** be in
the same namespace.

- URL is set everywhere, node still doesn't start

Double check that the `BareMetalHost` definition has `online` set to true. BMAC should take care of
this during the reconcile but, you know, software, computer gnomes, and dark magic.

- Node boots but it loooks like it is booting something else

Check that the `inspect.metal3.io` and `automatedCleaningMode` are both set to `disabled`. This will
prevent Ironic from doing inspection and any cleaning, which will speed up the deployment process
and prevent it from running IPA before running the ISO.

- Node boots, but nothing else seems to be happening

Check that an agent has been registered for this cluster and BMH. You can verify this by chekcing
the existing agents and find the one that has an interface with a MacAddress that matches the BMH
`BootMACAddress`.

If there is an agent, the next thing to check is that all validations have passed. This can be done
by inspecting the `ClusterDeployment` and verify that the validation phase has succeeded.
