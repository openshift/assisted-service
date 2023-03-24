# assisted-service

[![Swagger API](https://raw.githubusercontent.com/swagger-api/swagger-ui/master/src/img/logo_small.png)](https://petstore.swagger.io/?url=https://raw.githubusercontent.com/openshift/assisted-service/master/swagger.yaml)

[![Go Report Card](https://goreportcard.com/badge/github.com/openshift/assisted-service)](https://goreportcard.com/report/github.com/openshift/assisted-service)
[![License Apache](https://img.shields.io/github/license/openshift/assisted-service)](https://opensource.org/licenses/Apache-2.0)

## About

This repository provides a service that installs OpenShift.  Its main benefits include a minimum amount of prerequisites from the user's infrastructure, as well as comprehensive pre-flight validations to ensure a successful installation.  The service exposes either a REST API, or it can be deployed as an Operator where it exposes a Kubernetes-native API via Custom Resources.  A [UI](https://github.com/openshift-assisted/assisted-ui) is available that uses the REST API.

The Assisted Service can currently install clusters with highly-available control planes (3 hosts and above) and can also install Single-Node OpenShift (SNO).  Highly available clusters are configured to use OpenShift's `baremetal` platform (typically used in [bare metal IPI deployments](https://docs.openshift.com/container-platform/4.7/installing/installing_bare_metal_ipi/ipi-install-overview.html)), while SNO uses `none` (typically used in [UPI deployments](https://docs.openshift.com/container-platform/4.7/installing/installing_platform_agnostic/installing-platform-agnostic.html)).

The basic flow for creating a new OpenShift cluster using the Assisted Service via the UI or REST API is:
1. Create a new Cluster resource with the minimal required properties.
1. Generate and download a bootable image which is customized for that cluster. This image is based on RHCOS and is customized to automatically run an agent upon boot.
1. Boot the hosts that will form the cluster with the image from the previous step. The boot method is left to the user (e.g., USB drive, virtual media, PXE, etc.).
1. The agent running on each host contacts the Assisted Service via REST API and performs discovery (sends hardware inventory and connectivity information).
1. The UI guides the user through the installation, with the service performing validations along the way.  Alternatively, this can be done via API.
1. Once all validations pass, the user may initiate the installation.  Progress may be viewed via the UI or API, and logs are made available for download directly from the service.

## Demos and blog posts

Below are some recent demos and blog posts:
* Blog, Jan 2021: [Using the OpenShift Assisted Installer Service to Deploy an OpenShift Cluster on Bare Metal and vSphere](https://www.openshift.com/blog/using-the-openshift-assisted-installer-service-to-deploy-an-openshift-cluster-on-metal-and-vsphere)
* Blog and demo, Dec 2020: [Making OpenShift on Bare Metal easy](https://www.openshift.com/blog/making-openshift-on-bare-metal-easy)
* Blog and demo, Oct 2020: [It's Inside your House! Assisted Installer on Bare Metal Demonstration](https://www.openshift.com/blog/its-inside-the-house-assisted-installer-demonstration)

## User documentation

By continuing to read this document you will learn how to build and deploy Assisted Service. If you are interested in using Assisted Service to deploy an OCP cluster, please refer to the [User Documentation](docs/user-guide/README.md).

## Development Prerequisites

1. Docker or Podman. <br>
For podman make sure to [enable podman socket](docs/dev/podman.md) and update skipper.yaml to map it properly
1. skipper <https://github.com/stratoscale/skipper>
1. minikube (for tests)
1. kubectl
1. Python modules `pip install waiting`

## First Setup

To push your build target to a Docker registry you first need to change the default target.

1. Create a quay.io or Docker Hub account if you don't already have one. These instructions refer to quay.io, Docker Hub is similar.
1. Create a repository called assisted-service.
1. Make sure you have your `~/.docker/config.json` file set up to point to your account. For quay.io, you can go to quay.io -> User Settings, and click "Generate Encrypted Password" under "Docker CLI Password".
1. Login to quay.io using `docker login quay.io`.
1. Export the `SERVICE` environment variable to your Docker registry, and pass a tag of your choice, e.g., "test":

```sh
export SERVICE=quay.io/<username>/assisted-service:<tag>
```

For the first build of the build container run:

```shell
skipper build assisted-service-build
```

## Build

```shell
skipper make all
```

### Generate code after swagger changes

After every change in the API (`swagger.yaml`) the code should be generated and the build must pass.

```shell
skipper make generate-from-swagger
```

## Testing

More information is available here: [Assisted Installer Testing](docs/dev/testing.md)

## Update Discovery Image base OS

If you want to update the underlying operating system image used by the discovery iso, follow these steps:

1. Choose the base os image you want to use

   1. RHCOS: <https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/>
   2. Fedora CoreOS: <https://getfedora.org/en/coreos/download?tab=metal_virtualized&stream=stable>

2. Build the new iso generator image

   ```sh
   # Example with RHCOS
   BASE_OS_IMAGE=https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/latest/rhcos-4.6.0-0.nightly-2020-08-26-093617-x86_64-live.x86_64.iso make build-assisted-iso-generator-image
   ```

## Deployment

### Deploy to minikube

The deployment is a system deployment, it contains all the components the service need for all the operations to work (if implemented).
S3 service (minio), DB and will use the image generator to create the images in the deployed S3 and create relevant bucket in S3.

```shell
skipper make deploy-all
```

Note: when deployed in minikube, we need to be running [minikube tunnel](https://minikube.sigs.k8s.io/docs/handbook/accessing/#using-minikube-tunnel), as some services are type `LoadBalancer` and need to be exposed.

```shell
nohup minikube tunnel &>/dev/null
```


### Deploy to OpenShift

Besides default minikube deployment, the service supports deployment to OpenShift cluster using ingress as the access point to the service.

```shell
skipper make deploy-all TARGET=oc-ingress
```

This deployment option have multiple optional parameters that should be used in case you are not the Admin of the cluster:

1. `APPLY_NAMESPACE` - True by default. Will try to deploy "assisted-installer" namespace, if you are not the Admin of the cluster or maybe you don't have permissions for this operation you may skip namespace deployment.
1. `INGRESS_DOMAIN` - By default deployment script will try to get the domain prefix from OpenShift ingress controller. If you don't have access to it then you may specify the domain yourself. For example: `apps.ocp.prod.psi.redhat.com`
1. `DISABLE_TLS` - Based on the target environment, routes that are being used are HTTPS. Setting it to true will make HTTP routes instead.

To set the parameters simply add them in the end of the command, for example:

```shell
skipper make deploy-all TARGET=oc-ingress APPLY_NAMESPACE=False INGRESS_DOMAIN=apps.ocp.prod.psi.redhat.com
```

Note: All deployment configurations are under the `deploy` directory in case more detailed configuration is required.

### Deploy UI

This service supports optional UI deployment.

```shell
skipper make deploy-ui
```

\* In case you are using *podman* run the above command without `skipper`.

For OpenShift users, look at the service deployment options on OpenShift platform.

### Deploy Monitoring

Note: This target is only for development purpose.

This will allow you to deploy Prometheus and Grafana already integrated with Assisted installer:

- On Minikube

```shell
# Step by step
make deploy-olm
make deploy-prometheus
make deploy-grafana

# Or just all-in
make deploy-monitoring
```

- On Openshift

```shell
# Step by step
make deploy-prometheus TARGET=oc-ingress APPLY_NAMESPACE=false
make deploy-grafana TARGET=oc-ingress APPLY_NAMESPACE=false

# Or just all-in
make deploy-monitoring TARGET=oc-ingress APPLY_NAMESPACE=false
```

NOTE: To expose the monitoring UI's on your local environment you could follow these steps

```shell
kubectl config set-context $(kubectl config current-context) --namespace assisted-installer

# To expose Prometheus
kubectl port-forward svc/prometheus-k8s 9090:9090

# To expose Grafana
kubectl port-forward svc/grafana 3000:3000
```

Now you just need to access [http://127.0.0.1:3000](http://127.0.0.1:3000) to access to your Grafana deployment or [http://127.0.0.1:9090](http://127.0.0.1:9090) for Prometheus.

### Deploy by tag

This feature is for internal usage and not recommended to use by external users.
This option will select the required tag that will be used for each dependency.
If deploy-all use a new tag the update will be done automatically and there is no need to reboot/rollout any deployment.

Deploy images according to the manifest:

```shell
skipper make deploy-all DEPLOY_MANIFEST_PATH=./assisted-installer.yaml
```

Deploy images according to the manifest in the assisted-installer-deployment repo (require git tag/branch/hash):

```shell
skipper make deploy-all DEPLOY_MANIFEST_TAG=master
```

Deploy all the images with the same tag.
The tag is not validated, so you need to make sure it actually exists.

```shell
skipper make deploy-all DEPLOY_TAG=<tag>
```

Default tag is latest

### Deploy without a Kubernetes cluster

There are two ways the assisted service can be deployed without using a Kubernetes cluster:

#### Using containers on your local host
In this scenario the service and associated components are deployed onto your local host as a pod using Podman.

See the [README](./deploy/podman/) for details.

### Storage

assisted-service maintains a cache of openshift-baremetal-install binaries at
`$WORK_DIR/installercache/`. Persistent storage can optionally be mounted
there to persist the cache across container restarts. However, that storage
should not be shared across multiple assisted-service processes.

#### Cache Expiration

Currently there is no mechanism to expire openshift-baremetal-install binaries
out of the cache. The recommendation for now is to allow the cache to use the
container's own local storage that will vanish when the Pod gets replaced, for
example during upgrade. That will prevent the cache from growing forever while
allowing it to be effective most of the time.

## Troubleshooting

A document that can assist troubleshooting: [link](https://docs.google.com/document/d/1WDc5LQjNnqpznM9YFTGb9Bg1kqPVckgGepS4KBxGSqw)

## Documentation

Markdown formatted documentation is available in the [docs](docs) directory.

## Linked repositories

### coreos_installation_iso

<https://github.com/oshercc/coreos_installation_iso>

Image in charge of generating the Fedora-coreOs image used to install the host with the relevant ignition file.\
Image is uploaded to deployed S3 under the name template "installer-image-\<cluster-id\>".

## Assisted Service on console.redhat.com
The Assisted Installer is also available for users as a SAAS hosted in console.redhat.com.

More information is available here: [Assisted Installer on console.redhat.com](docs/cloud.md)

## Setting a custom discovery ISO password
It's possible to modify the discovery ISO (via the API) to enable password login for troubleshooting purposes.

More information is available here: [Set discovery ISO user password example](docs/set-discovery-password.md)

## Contributing

Please, read our [CONTRIBUTING] guidelines for more info about how to create, document, and review PRs.

[CONTRIBUTING]: https://github.com/openshift/assisted-service/blob/master/CONTRIBUTING.md
