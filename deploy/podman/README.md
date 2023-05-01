# Deploy with Podman

In this scenario the service and associated components are deployed onto your
local host in containers using [Podman](https://podman.io/).

## General Requirements

Make sure you have [Podman](https://podman.io) version 3.3+ installed. If you
must use an older version of podman, reference the [previous documentation and
procedure](https://github.com/openshift/assisted-service/tree/v2.0.11#deploy-without-a-kubernetes-cluster)
to avoid a [podman bug](https://github.com/containers/podman/issues/9609).

## Requirements for an HTTP based deployment

### Files

No need to clone the whole repo. Use:
* [pod.yml](./pod.yml)
* [configmap.yml](configmap.yml)

### Environment Variables

Change `IMAGE_SERVICE_BASE_URL` and `SERVICE_BASE_URL` in
[configmap.yml](./configmap.yml) to match the hostname or IP address of your
host. For example if your IP address is 192.168.122.2, then the
`SERVICE_BASE_URL` would be set to <http://192.168.122.2:8090>. Port 8090 is
the assisted-service API that agents will connect to.

## Requirements for an HTTPS based deployment

* Assisted UI supports HTTPS starting from UI version [v2.10.0](https://github.com/openshift-assisted/assisted-installer-ui/releases/tag/v2.10.0)

* These instructions will secure the following communication paths:
    1. assisted-installer-ui :left_right_arrow: assisted-service
    2. User API queries :left_right_arrow: assisted-service
    3. assisted-image-service :left_right_arrow: assisted-service
    4. discovery agents :left_right_arrow: assisted-service
    5. controller pod :left_right_arrow: assisted-service

### TLS Certificates

#### Generate

```shell
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout tls.key -out tls.crt -subj "/CN=localhost" -addext 'subjectAltName = IP:127.0.0.1'
```
#### Embed to a ConfigMap

Embed both key and certificate in [configmap_tls_certs.yml](configmap_tls_certs.yml)
Those will get mounted to `assisted-installer-service`, `assisted-installer-image-service` and `assisted-installer-ui` for encryption.

### Files

No need to clone the whole repo. Use:
* [pod_tls.yml](./pod_tls.yml)
* [configmap_tls.yml](configmap_tls.yml)
* [configmap_tls_certs.yml](configmap_tls_certs.yml)

### Environment Variables

Change `IMAGE_SERVICE_BASE_URL` and `SERVICE_BASE_URL` in
[configmap_tls.yml](./configmap_tls.yml) to match the hostname or IP address of your
host. For example if your IP address is 192.168.122.2, then the
`SERVICE_BASE_URL` would be set to <https://192.168.122.2:8090>. Port 8090 is
the assisted-service API that agents will connect to.

Additionally, make sure to change the following:
```yaml
ASSISTED_SERVICE_URL: https://127.0.0.1:8090  
HTTPS_CERT_FILE: "/etc/certs/tls.crt" # Need to match certs-configmap-volume mountPath
HTTPS_KEY_FILE: "/etc/certs/tls.key"  # Need to match certs-configmap-volume mountPath
HTTPS_CA_FILE: "/etc/certs/tls.crt"   # Need to match certs-configmap-volume mountPath. Needed if the certificate is not signed by a CA in RHEL's default trust bundle.
ASSISTED_SERVICE_HOST: 127.0.0.1:8090
ASSISTED_SERVICE_SCHEME: https # A must for the UI nginx to be configured with TLS
```

## Optional Configuration

Other environment variables may be set in configmap.yml. For example, custom
agent (`AGENT_DOCKER_IMAGE`), installer (`INSTALLER_IMAGE`) and controller
(`CONTROLLER_IMAGE`) images can be defined.

## Run it

### If HTTP based 

```shell
podman play kube --configmap configmap.yml pod.yml
```

### If HTTPS based

```shell
podman play kube --configmap configmap_tls_certs.yml --configmap configmap_tls.yml pod_tls.yml
```

To preserve data about existing clusters between pod reconfigurations the
`pod-persistent.yml` manifest could be use instead, which creates additional
volumes for database and cluster's artifacts:

```shell
podman play kube --configmap configmap.yml pod-persistent.yml
```

If you only want to provision a single cluster at a time, which is a common
use case, persistence is not necessary.

The UI will available at: `http://<host-ip-address>:8080`

## Remove it

```shell
podman play kube --down pod.yml
```

## OKD configuration

Assisted Service can install OKD clusters using a different set of parameters:
```shell
podman play kube --configmap okd-configmap.yml pod.yml
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
