[![Actions Status](https://github.com/openshift/assisted-service/workflows/unit-test/badge.svg)](https://github.com/openshift/assisted-service/actions)


[![Actions Status](https://raw.githubusercontent.com/swagger-api/swagger-ui/master/src/img/logo_small.png)](https://generator.swagger.io/?url=https://raw.githubusercontent.com/openshift/assisted-service/master/swagger.yaml)


# assisted-service

## Prerequisites

1. Docker lala
1. skipper https://github.com/stratoscale/skipper
1. minikube (for tests)
1. kubectl


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

## Test

#### Pre-configuration
  - Run minikube on your system.
  - Deploy services `skipper make deploy-test`

### Run system tests

```shell
skipper make test
```

### Run system tests with regex

```shell
skipper make test FOCUS=versions
```

### Run only unit tests

```shell
skipper make unit-test
```

### Run unit tests for specific package

```shell
skipper make unit-test TEST=./internal/host
```

### Run unit tests with regex

```shell
skipper make unit-test FOCUS=cluster
```

### Update service for the subsystem tests

if you are making changes and don't want to deploy everything once again you can simply run this command:

```shell
skipper make update && kubectl get pod --namespace assisted-installer -o name | grep assisted-service | xargs kubectl delete --namespace assisted-installer
```

It will build and push a new image of the service to your Docker registry, then delete the service pod from minikube, the deployment will handle the update and pull the new image to start the service again.

## Update Discovery Image base OS

If you want to update the underlying operating system image used by the discovery iso, follow these steps:

1. Choose the base os image you want to use

   1. RHCOS: https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/
   2. Fedora CoreOS: https://getfedora.org/en/coreos/download?tab=metal_virtualized&stream=stable

2. Build the new iso generator image

   ```sh
   # Example with RHCOS
   BASE_OS_IMAGE=https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/pre-release/latest/rhcos-4.6.0-0.nightly-2020-08-26-093617-x86_64-live.x86_64.iso make build-assisted-iso-generator-image
   ```

## Deployment

### Deploy to minikube

The deployment is a system deployment, it contains all the components the service need for all the operations to work (if implemented).
S3 service (scality), DB and will use the image generator to create the images in the deployed S3 and create relevant bucket in S3.

```shell
skipper make deploy-all
```

### Deploy to OpenShift

Besides default minikube deployment, the service support deployment to OpenShift cluster using ingress as the access point to the service.

```shell
skipper make deploy-all TARGET=oc-ingress
```

This deployment option have multiple optional parameters that should be used in case you are not the Admin of the cluster:
1. `APPLY_NAMESPACE` - True by default. Will try to deploy "assisted-installer" namespace, if you are not the Admin of the cluster or maybe you don't have permissions for this operation you may skip namespace deployment.
1. `INGRESS_DOMAIN` - By default deployment script will try to get the domain prefix from OpenShift ingress controller. If you don't have access to it then you may specify the domain yourself. For example: `apps.ocp.prod.psi.redhat.com`

To set the parameters simply add them in the end of the command, for example:
```shell
skipper make deploy-all TARGET=oc-ingress APPLY_NAMESPACE=False INGRESS_DOMAIN=apps.ocp.prod.psi.redhat.com
```

Note: All deployment configurations are under the `deploy` directory in case more detailed configuration is required.

### Deploy UI

This service support optional UI deployment.
```shell
skipper make deploy-ui
```

* In case you are using *podman* run the above command without `skipper`.


For OpenShift users, look at the service deployment options on OpenShift platform.

### Deploy Monitoring

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
```
skipper make deploy-all DEPLOY_MANIFEST_PATH=./assisted-installer.yaml
```

Deploy images according to the manifest in the assisted-installer-deployment repo (require git tag/branch/hash):
```
skipper make deploy-all DEPLOY_MANIFEST_TAG=master
```

Deploy all the images with the same tag.
The tag is not validated, so you need to make sure it actually exists.
```
skipper make deploy-all DEPLOY_TAG=<tag>
```

Default tag is latest


### Deploy without a Kubernetes cluster

The assisted service can also be deployed without using a Kubernetes cluster. In this scenario the service and associated components are deployed onto your local host as a pod using Podman.

This type of deployment requires a different container image that combines components that are used to generate the installer ISO and configuration files. First build the image:

```
export SERVICE=quay.io/<your-org>/assisted-service:latest
make build-onprem
```

To deploy, update SERVICE_BASE_URL in the onprem-environment file to match the hostname or IP address of your host. For example if your IP address is 192.168.122.2, then the SERVICE_BASE_URL would be set to http://192.168.122.2:8090. Port 8090 is the assisted-service API.

Then deploy the containers:

```
make deploy-onprem
```

Check all containers are up and running:

```
podman ps -a
```

The UI will available at:

```
https://<host-ip-address>:8443
```

To remove the containers:

```
make clean-onprem
```

To run the subsystem tests:

```
make test-onprem
```

### Storage

assisted-service maintains a cache of openshift-baremetal-install binaries at
`$WORK_DIR/installercache/`. Persistent storage can optionally be mounted
there to persist the cache accross container restarts. However, that storage
should not be shared accross multiple assisted-service processes.

#### Cache Expiration

Currently there is no mechanism to expire openshift-baremetal-install binaries
out of the cache. The recommendation for now is to allow the cache to use the
container's own local storage that will vanish when the Pod gets replaced, for
example during upgrade. That will prevent the cache from growing forever while
allowing it to be effective most of the time.

## Troubleshooting

A document that can assist troubleshooting: [link](https://docs.google.com/document/d/1WDc5LQjNnqpznM9YFTGb9Bg1kqPVckgGepS4KBxGSqw)

## Documentation

To rebuild the site after adding some documentation to the Markdown files, you just need to execute this Make target before the push

```shell
make docs
```

To validate the documentation generated, go to the root of the repo and execute

```shell
make docs_serve
```

After that, you just need to access to [127.0.0.1:8000](http://127.0.0.1:8000/) on your browser and check the new content.

_NOTE: To use these features, you need to have mkdocs installed in your system, to do that you just need to execute this command `pip3 install --user mkdocs`_

##  Linked repositories
* #### coreos_installation_iso:
    https://github.com/oshercc/coreos_installation_iso

    Image in charge of generating the Fedora-coreOs image used to install the host with the relevant ignition file.\
    Image is uploaded to deployed S3 under the name template "installer-image-<cluster-id>".
