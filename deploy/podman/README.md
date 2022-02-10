# Deploy with podman

In this scenario the service and associated components are deployed onto your
local host in containers using [Podman](https://podman.io/).

## Requirements

Make sure you have [podman](https://podman.io) installed.

Grab [pod.yml](./pod.yml) and [configmap.yml](configmap.yml) from this
directory. No need to clone the whole repo.

Change `SERVICE_BASE_URL` in [configmap.yml](./configmap.yml) to match the
hostname or IP address of your host. For example if your IP address is
192.168.122.2, then the `SERVICE_BASE_URL` would be set to
<http://192.168.122.2:8090>. Port 8090 is the assisted-service API that agents
will connect to.

## Run it

```shell
podman play kube --configmap configmap.yml pod.yml
```

The UI will available at: `http://<host-ip-address>:8080`

## Remove it

```shell
podman play kube --down pod.yml
```

## Optional Configuration

Other environment variables may be set in configmap.yml. For example, custom
agent (`AGENT_DOCKER_IMAGE`), installer (`INSTALLER_IMAGE`) and controller
(`CONTROLLER_IMAGE`) images can be defined.

## OKD configuration

Assisted Service can install OKD clusters using a different set of parameters:
```shell
podman play kube --configmap okd_configmap.yml pod.yml
```
or
```shell
make deploy-onprem OKD=true
```
for developers

Configuration differences are:
* `OS_IMAGES` should point to Fedora CoreOS (see [Fedora CoreOS Release artifacts](https://getfedora.org/en/coreos/download?tab=metal_virtualized&stream=stable&arch=x86_64))
* `RELEASE_IMAGES` lists available OKD versions (see [OKD Releases](https://github.com/openshift/okd/releases))
* `OKD_RPMS_IMAGE` is additional image containing Kubelet/CRI-O RPMs (see [example repo](https://github.com/vrutkovs/okd-rpms))
