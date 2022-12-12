# Assisted Service Operator

This directory includes two main actions:

- Assisted Service operator installation workflow, including
  installation of Local Storage Operator and Hive Operator.
- ZTP workflow of spoke clusters.

## Dependencies

Operator installation requires an OCP 4.8 cluster as the "Hub Cluster".
Also, ZTP flow requires a node with enough CPU cores, memory and disk size
which is connected to vBMC system.
In order to have a workable setup, you can use
[dev-scripts](https://github.com/openshift-metal3/dev-scripts) with the following configurations:

```
IP_STACK=v4  # disconnected env is not yet fully supported

# ZTP-related configurations:

# This will define our single-node host, which is eligible
# for installation by assisted-service standards
NUM_EXTRA_WORKERS=1
EXTRA_WORKER_VCPU=8
EXTRA_WORKER_MEMORY=16384
EXTRA_WORKER_DISK=120

# This will enable us provisioning BMH by BMAC with the
# redfish-virtualmedia driver, as well as enabling
# rebooting by assisted-installer
PROVISIONING_NETWORK_PROFILE=Disabled
REDFISH_EMULATOR_IGNORE_BOOT_DEVICE=True
```

## Operator Installation

A complete installation of hub-cluster consists on the following:

- Setting up several (virtual) disks for persistent storage.
- Installing Local Storage Operator and creating a storage class.
- Installing Hive Operator.
- Installing Assisted Service Operator.
- Configuring BMO to watch all namespaces searching for BMH objects.

Installation of the operator is pretty simple:

```
# replace with path in your system for any eligible cluster auth:
export KUBECONFIG=/home/test/dev-scripts/ocp/ostest/auth/kubeconfig

cd deploy/operator/
./deploy.sh
```

By default, this will define sdb,sdc,...,sdf disks on workers if present,
or on master nodes if there are no dedicated worker nodes. If you want to
control which disks are being created, use:

```
DISKS=$(echo sd{b..d}) ./deploy.sh
```

If you want to skip LSO installation (in case LSO is already installed), use:
Some other configurations are also available:

```
export INSTALL_LSO=false  # in case LSO is already installed
export STORAGE_CLASS_NAME=storage-class  # if you want to define this name by yourself
./deploy.sh
```

## Running ZTP Flow (with BMH, BMAC, and other friends)

Again, it's quite easy:

```
# replace with your paths:
export ASSISTED_PULLSECRET_JSON=/home/test/dev-scripts/pull_secret.json
export EXTRA_BAREMETALHOSTS_FILE=/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json

cd deploy/operator/ztp/
./deploy_spoke_cluster.sh
```

The following actions are happening in this script:

- Secrets for pull-secret and for private SSH key will be created.
- A BMH object will be created for the extra host specified on the provided json file.
- The following objects will be created as well: cluster-deployment, infra-env,
  cluster-image-set, agent-cluster-install.
- It will wait for an agent object to get created, indicating the host has joined the cluster.
- It will wait for the installation to successfully pass.

You can customize this script with the following arguments:

```
export ASSISTED_NAMESPACE=assisted-installer
export ASSISTED_CLUSTER_NAME=assisted-test-cluster
export DS_OPENSHIFT_VERSION=openshift-v4.8.0  # this will be the name of the cluster-image-set object
export OPENSHIFT_INSTALL_RELEASE_IMAGE=quay.io/openshift-release-dev/ocp-release:4.8.0-fc.3-x86_64
export ASSISTED_CLUSTER_DEPLOYMENT_NAME=assisted-test-cluster
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME=assisted-agent-cluster-install
export ASSISTED_INFRAENV_NAME=assisted-infra-env
export ASSISTED_PULLSECRET_NAME=assisted-pull-secret
export ASSISTED_PRIVATEKEY_NAME=assisted-ssh-private-key
export SPOKE_CONTROLPLANE_AGENTS=1  # currently only single-node is supported
```

## Running None Platform ZTP Flow (Testing only)

Create ZTP installation flow for None platform. For this the following changes were needed:

- Add user-managed-networking variable support for this flow as it is needed by assisted service
- Remove API and Ingress VIPs from agentclusterinstall YAML file when running this flow.
- Add load balancer on top of nginx for none platform use
- Add support for DNS definition in libvirt that adds DNS names needed by Openshift to complete none platform installation.

The following environment variables were added to support this flow:

```
# Set to true to use none platform
export USER_MANAGED_NETWORKING="${USER_MANAGED_NETWORKING:-false}"

# Spawn load balancer for none platform on local machine
export SPAWN_NONE_PLATFORM_LOAD_BALANCER="${SPAWN_NONE_PLATFORM_LOAD_BALANCER:-false}"

# Add DNS entrries in LIBVIRT network to point to the load balancer IP address
export ADD_NONE_PLATFORM_LIBVIRT_DNS="${ADD_NONE_PLATFORM_LIBVIRT_DNS:-false}"

# Name of none platform network to add the DNS entries
export LIBVIRT_NONE_PLATFORM_NETWORK="${LIBVIRT_NONE_PLATFORM_NETWORK:-ostestbm}"

# The load balancer IP address
export LOAD_BALANCER_IP="${LOAD_BALANCER_IP:-192.168.111.1}"
```

## Installing day2 nodes

In order to install day2 nodes for ZTP flow run:

```
# replace with your paths
export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"

# The name of the infraenv.  Used to extract the download URL.
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"

cd deploy/operator/ztp/
./add_day2_remote_nodes
```

The following is an example of a JSON formatted file referenced by REMOTE_BAREMETALHOSTS_FILE env varible:

```
[
  {
    "name": "ostest-extraworker-4",
    "driver": "redfish-virtualmedia",
    "resource_class": "baremetal",
    "driver_info": {
      "username": "admin",
      "password": "password",
      "port": "8000",
      "address": "redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/2bc99f91-b336-466c-ad3b-74759fda2ff7",
      "deploy_kernel": "http:///images/ironic-python-agent.kernel",
      "deploy_ramdisk": "http:///images/ironic-python-agent.initramfs"
    },
    "ports": [
      {
        "address": "00:81:03:04:b2:56",
        "pxe_enabled": true
      }
    ],
    "properties": {
      "local_gb": "120",
      "cpu_arch": "x86_64"
    }
  }
]
```

The script will attempt to install all the nodes that appear in this file
