# Multi-arch and heterogeneous OpenShift clusters

## Glossary

* multi-arch release payload - OpenShift release image that supports more than a single CPU architecture. E.g. `quay.io/openshift-release-dev/ocp-release:4.11.10-x86_64` is to be used with x86_64 only, whereas `quay.io/openshift-release-dev/ocp-release:4.11.10-multi` can be used for other architectures too.

* Heterogeneous cluster - OpenShift cluster consisting of Nodes of multiple architectures. E.g. a cluster with control plane running on x86_64 and worker nodes running on arm64.

> NOTE: You need to use multi-arch release payload in order to install heterogeneous cluster. It is however not forbidden to use multi-arch release payload for installing a cluster that uses only single architecture.

## Prerequisites

In order to use the multi-arch release images in case of an on-prem deployment, you need to run the Service with the following configuration:

```
OS_IMAGES: '[{"openshift_version":"4.11","cpu_architecture":"x86_64","url":"https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-x86_64-live.x86_64.iso","version":"411.86.202210041459-0"},{"openshift_version":"4.11","cpu_architecture":"arm64","url":"https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.11/4.11.9/rhcos-4.11.9-aarch64-live.aarch64.iso","version":"411.86.202210032347-0"}]'

RELEASE_IMAGES: '[{"openshift_version":"4.11.0-multi","cpu_architecture":"multi","cpu_architectures":["x86_64","arm64","ppc64le","s390x"],"url":"quay.io/openshift-release-dev/ocp-release:4.11.0-multi","version":"4.11.0-multi"}]'

AGENT_DOCKER_IMAGE: quay.io/edge-infrastructure/assisted-installer-agent-multi:latest
CONTROLLER_IMAGE: quay.io/edge-infrastructure/assisted-installer-controller-multi:latest
INSTALLER_IMAGE: quay.io/edge-infrastructure/assisted-installer-multi:latest
```

> NOTE: In the above the OS images contain separate entry for every architecture that should be supported. This is because there exists no multi-arch RHCOS image. In the scope of this feature are only container images.

In case of an on-prem deployment using Infrastructure Operator, the multi-arch release image can be added by creating ClusterImageSet CR:

```
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
  name: openshift-v4.11
spec:
  releaseImage: quay.io/openshift-release-dev/ocp-release:4.11.0-multi
```

When using the SaaS offering via cloud.redhat.com, no any additional configuration is required.

## Setup the cluster

Clusters created using Assisted Installer internally have a property `cpu_architecture` configured. In a regular single-arch configuration this should be configured as desired (e.g. `x86_64`, `arm64` or empty for a default value). When multi-arch release image is used, the service will internally fill this value with `multi`.

Therefore, the process of creating a cluster is the same whether single-arch or multi-arch is used. Everything is controlled only by selecting a desired OpenShift Version.

An example part of the cluster object created by selecting `4.11.0-multi` is shown below:

```
{
  "cpu_architecture": "multi",
  "email_domain": "redhat.com",
  "id": "54afd8b9-8cc7-4194-b7d0-44619420adf6",
  "kind": "Cluster",
  "ocp_release_image": "quay.io/openshift-release-dev/ocp-release:4.11.0-multi",
  "openshift_version": "4.11.0-multi",
}
```

## Create infrastructure environment

When using SaaS using the UI or aicli tool, this step can be skipped as InfraEnvs are created automatically. In any other case you should create an InfraEnv in order to get a bootable ISO. It is important to note that `cpu_architecture` field must contain a real architecture (not `multi`) as there is no multi-arch RHCOS image.

An example part of the InfraEnv object is shown below:

```
{
  "cluster_id": "54afd8b9-8cc7-4194-b7d0-44619420adf6",
  "cpu_architecture": "x86_64",
  "email_domain": "redhat.com",
  "id": "3877636a-eed0-4aeb-a9e2-33720393374a",
  "kind": "InfraEnv",
  "name": "chocobomb_infra-env",
  "openshift_version": "4.11",
}
```

In order to generate a new ISO for another architecture, the command below can be used:

```
aicli create infraenv ${CLUSTER_NAME}-arm \
-P cluster=$CLUSTER_NAME \
-P pull_secret=/local/pull_secret.json \
-P cpu_architecture=arm64
```

## Boot the nodes

Once cluster and InfraEnv is created, the nodes can be booted using the generated ISO. This process is no different from the regular host discovery known from single-arch topologies. Once the hosts are discovered, the installation can begin as usual.

> NOTE: It is required for all the control plane nodes to be of the same architecture.

## Add machines of a different architecture

Once the cluster is installed, nodes of a different architectures can be added as soon as the cluster is in "Add Hosts" state. Using the SaaS UI it can be done using the button in the UI. In case of a deployment using Infrastructure Operator no further action is needed as the cluster gets into the required state automatically.

Please make sure you have ISO for a desired architecture of additional workers (using steps described above) and follow a regular process of adding day-2 workers. The `"cpu_architecture": "multi"` parameter of the Cluster object ensures that there will be no validation ensuring that all the nodes have the same architecture. In case when the Cluster object has a different value of `cpu_architecture`, any additional machine needs to have an architecture that matches the cluster.
