# Quick start guide for Assisted Service Developer

Over time, assisted-service has been adapted to different scenarios, and has become one more piece of many other subsystems in which it works integrated with other Openshift components.

For this reason, depending on the development we are working on, we have different scenarios for developers that allow us greater flexibility for our daily work. For example, there will be cases in which we are only interested in doing some specific tests on the API and this can be covered with a light environment, but in other cases we may need a complete Openshift cluster with its control plane and worker nodes, in which case the scenario will be more complex and heavy.

The frequently used scenarios are described below.

## Testing simple changes to the REST service
[Detailed documentation](../../deploy/podman/README.md)

In this scenario the goal is to run small tests with the assisted-installer REST API. This can be done in a local environment by starting the assisted-service with podman.

We can start it directly with make:
```shell
make deploy-onprem
```

## Testing e2e-flow with REST
[Detailed documentation](https://github.com/openshift/assisted-test-infra/blob/master/README.md)

This scenario is useful for developing and testing our end-to-end tests.
This environment requires a large amount of resources since it will deploy additional baremetal hosts (VMs), so **running it in local environments is not recommended**.

We can start this scenario from the `assisted-test-infra` project:
```shell
make run
```

## Testing kube-api features
[Detailed documentation](http://crc.dev/crc/getting_started/getting_started/introducing/)

The goal here would be to test the operation of the resources associated with assisted-installer through the kube-api. This is a lightweight scenario and we could work with a local environment.

We can install a local environment (Openshift / OKD / Microshift) with the **crc** tool. There is a section to perform this type of installation in the [Openshift console](https://console.redhat.com/openshift/create/local).

## Testing infrastructure operator
[Detailed documentation](operator-on-kind.md)  
[Development environment using kind](kind/README.md)

In this scenario we will have an environment with the infrastructure operator, which will be in charge of managing assisted-service and assisted-image-service. This scenario will be running in a local environment with a kind cluster, it is lightweight and its deployment is done in a few minutes. Despite being lightweight, it is a fairly complete environment so it would also be valid for other types of tests such as APIs.

We can start it directly from make:
```shell
make deploy-dev-infra
```

## Testing Cluster-API (sylva/CAPI)
[Development environment using kind](kind/README.md)

If you are involved in the Cluster API (CAPI) project, which is responsible for the k8s cluster lifecycle management subsystem, you have a small development environment deployed in a local kind environment that will allow you to test it.

If test clusters are to be deployed, additional work will be required, this section is pending development.

To start this environment:
```shell
make deploy-sylva
```

## Full Openshift cluster with Metal³ (dev-scripts)
[Detailed documentation](https://github.com/openshift-metal3/dev-scripts)

This project, developed and maintained primarily by the metal³ group, deploys an environment with baremetal nodes on VMs. It is a generic project that works for M3 and can also be used for testing with assisted-installer, although it has some disadvantages:
* It is a resource-intensive environment, at least 64 GB of memory and 80 GB of disk space, so it is not recommended for local environments.
* It is a fairly complex project, designed to cover a lot of needs that we may not use, so its configuration is also quite complex.
* The provisioning process can take a couple of hours.

If you need to work in this scenario you should follow these steps:
1. Set up a system to host it, as explained [here](https://github.com/openshift-metal3/dev-scripts/tree/master?tab=readme-ov-file#instructions).
2. Deploy the assisted-service operator. [This document](../../deploy/operator/README.md) explains how to do this.
