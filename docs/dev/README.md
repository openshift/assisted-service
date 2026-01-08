# Quick start guide for Assisted Service Developer

Over time, assisted-service has been adapted to different scenarios, and has become one more piece of many other subsystems in which it works integrated with other Openshift components.

For this reason, depending on the development we are working on, we have different scenarios for developers that allow us greater flexibility for our daily work. For example, there will be cases in which we are only interested in doing some specific tests on the API and this can be covered with a light environment, but in other cases we may need a complete Openshift cluster with its control plane and worker nodes, in which case the scenario will be more complex and heavy.

The frequently used scenarios are described below.

| tool | resource usage | run it locally | useful for |
| :---: | :---: | :---: | :---: |
| [podman](##podman) | light | :white_check_mark: | small validations of API REST service |
| [crc](##crc) | medium | :white_check_mark: | tests with a local SNO Openshift/Microshift cluster |
| [assisted-test-infra](##assisted-test-infra) | heavy | :x: | testing and development of end-to-end tests |
| [kind](#kind) | light / heavy | :x: | this should be valid to any test related with assisted-installer |
| [dev-scripts](##dev-scripts) | heavy | :x: | all-road environment. Anything should work here |

## podman
[Detailed documentation](../../deploy/podman/README.md)  
[REST-API V2 - Getting Started Guide](../docs/user-guide/rest-api-getting-started.md)

In this scenario the goal is to run small tests with the assisted-installer REST API. This can be done in a local environment by starting the assisted-service with podman.

During assisted-image-service startup, the images configured in the `OS_IMAGES` and `RELEASE_IMAGES` variables present in the [deploy/podman/configmap.yml](../../deploy/podman/configmap.yml) file will be downloaded. By default, these values ​​contain Openshift versions from 4.9 onwards. You may want to define a smaller set to improve resource usage. For example:
```yaml
OS_IMAGES: '[{"openshift_version":"4.16","cpu_architecture":"x86_64","url":"https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.16/4.16.3/rhcos-4.16.3-x86_64-live.x86_64.iso","version":"416.94.202406251923-0"}]'
RELEASE_IMAGES: '[{"openshift_version":"4.16","cpu_architecture":"x86_64","cpu_architectures":["x86_64"],"url":"quay.io/openshift-release-dev/ocp-release:4.16.10-x86_64","version":"4.16.10","default":true}]' 
```

We can start this local environment using make:
```shell
make deploy-onprem
```
Once finished, we can see the containers running:
```shell
$ podman ps

CONTAINER ID  IMAGE                                                      COMMAND               CREATED        STATUS        PORTS                                                                   NAMES
487e6c1bdb9a  localhost/podman-pause:4.9.3-1708357294                                          4 minutes ago  Up 3 minutes  0.0.0.0:8080->8080/tcp, 0.0.0.0:8090->8090/tcp, 0.0.0.0:8888->8888/tcp  7c18ebd0915a-infra
8479b5eb8a8d  quay.io/sclorg/postgresql-13-c9s:latest                    run-postgresql        4 minutes ago  Up 3 minutes  0.0.0.0:8080->8080/tcp, 0.0.0.0:8090->8090/tcp, 0.0.0.0:8888->8888/tcp  assisted-installer-db
ffb9013c4fab  quay.io/edge-infrastructure/assisted-installer-ui:latest   /deploy/start.sh      3 minutes ago  Up 3 minutes  0.0.0.0:8080->8080/tcp, 0.0.0.0:8090->8090/tcp, 0.0.0.0:8888->8888/tcp  assisted-installer-ui
100c865abfd6  quay.io/edge-infrastructure/assisted-image-service:latest  /assisted-image-s...  3 minutes ago  Up 3 minutes  0.0.0.0:8080->8080/tcp, 0.0.0.0:8090->8090/tcp, 0.0.0.0:8888->8888/tcp  assisted-installer-image-service
78924b68f7af  quay.io/edge-infrastructure/assisted-service:latest        /assisted-service     3 minutes ago  Up 3 minutes  0.0.0.0:8080->8080/tcp, 0.0.0.0:8090->8090/tcp, 0.0.0.0:8888->8888/tcp  assisted-installer-service
```
For testing we can use `curl` (or [aicli](https://aicli.readthedocs.io/en/latest/) for more complex flows):
```shell
$ curl -i http://127.0.0.1:8090/api/assisted-install/v2/infra-envs
HTTP/1.1 200 OK
Content-Type: application/json
Vary: Accept-Encoding
Date: Wed, 11 Sep 2024 14:16:43 GMT
Content-Length: 3

[]

$ curl -i http://127.0.0.1:8090/api/assisted-install/v2/clusters
HTTP/1.1 200 OK
Content-Type: application/json
Vary: Accept-Encoding
Date: Wed, 11 Sep 2024 14:18:59 GMT
Content-Length: 3

[]
```
A more detailed description of how to use the REST API is available in the [user guide](docs/user-guide/rest-api-getting-started.md).

## assisted-test-infra
[Detailed documentation](https://github.com/openshift/assisted-test-infra/blob/master/README.md)

This scenario is useful for developing and testing our end-to-end tests.
This environment requires a large amount of resources since it will deploy additional baremetal hosts (VMs), so **running it in local environments is not recommended, do it on a beaker box or an ephemeral server**.

This environment requires a prior setup on the machine where it will run. The steps for installation are described in [this guide](https://github.com/openshift/assisted-test-infra/blob/master/GUIDE.md).

## CRC
[Detailed documentation](http://crc.dev/crc/getting_started/getting_started/introducing/)

We can install a local environment (Openshift / OKD / Microshift) with the **crc** tool. `crc` deploys a SNO cluster in our local environment and provides us a complete environment where we can perform our tests. By having all (or most) of Openshift components, our work here could go beyond the assisted-service functionalities and test its integration with other components.

There is a section to perform this type of installation in the [Openshift console](https://console.redhat.com/openshift/create/local).

## kind
[Detailed documentation](operator-on-kind.md)  
[Development environment using kind](kind/README.md)

In this scenario we will have an environment with `kind` where we will deploy the infrastructure operator, which will be in charge of managing assisted-service and assisted-image-service. This scenario is lightweight and its deployment is done in a few minutes. Despite being lightweight, it is a fairly complete environment so it would also be valid for other types of tests such as APIs.

We can start it directly from make:
```shell
make deploy-dev-infra
```

## dev-scripts
[Detailed documentation](https://github.com/openshift-metal3/dev-scripts)

This project, developed and maintained primarily by the metal³ group, deploys an environment with baremetal nodes on VMs. It is a generic project that works for M3 and can also be used for testing with assisted-installer, although it has some disadvantages:
* It is a resource-intensive environment, at least 64 GB of memory and 80 GB of disk space, so it is not recommended for local environments.
* It is a fairly complex project, designed to cover a lot of needs that we may not use, so its configuration is also quite complex.
* The provisioning process can take a couple of hours.

If you need to work in this scenario you should follow these steps:
1. Set up a system to host it, as explained [here](https://github.com/openshift-metal3/dev-scripts/tree/master?tab=readme-ov-file#instructions).
2. Deploy the assisted-service operator. [This document](../../deploy/operator/README.md) explains how to do this.
