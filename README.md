[![Actions Status](https://github.com/filanov/bm-inventory/workflows/unit-test/badge.svg)](https://github.com/filanov/bm-inventory/actions)


[![Actions Status](https://raw.githubusercontent.com/swagger-api/swagger-ui/master/src/img/logo_small.png)](https://filanov.github.io/bm-inventory/)
# bm-inventory

## Prerequisites

1. Docker
1. skipper https://github.com/stratoscale/skipper
1. minikube (for tests)
1. kubectl


## First Setup

To push your build target to a Docker registry you first need to change the default target.
1. Create a quay.io or Docker Hub account if you don't already have one. These instructions refer to quay.io, Docker Hub is similar.
1. Create a repository called bm-inventory.
1. Make sure you have your `~/.docker/config.json` file set up to point to your account. For quay.io, you can go to quay.io -> User Settings, and click "Generate Encrypted Password" under "Docker CLI Password".
1. Login to quay.io using `docker login quay.io`.
1. Export the `SERVICE` environment variable to your Docker registry, and pass a tag of your choice, e.g., "test":

```sh
export SERVICE=quay.io/<username>/bm-inventory:<tag>
```

For the first build of the build container run:
```shell
skipper build bm-inventory-build
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

### Update service for the subsystem tests

if you are making changes and don't want to deploy everything once again you can simply run this command:

```shell
skipper make update && kubectl get pod --namespace assisted-installer -o name | grep bm-inventory | xargs kubectl delete --namespace assisted-installer
```

It will build and push a new image of the service to your Docker registry, then delete the service pod from minikube, the deployment will handle the update and pull the new image to start the service again.

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

To set the parameters simply add them in the end of the command, for example
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
    make deploy-prometheus TARGET=oc-ingress
    make deploy-grafana TARGET=oc-ingress

    # Or just all-in
    make deploy-monitoring TARGET=oc-ingress
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


## Troubleshooting

A document that can assist troubleshooting: [link](https://docs.google.com/document/d/1WDc5LQjNnqpznM9YFTGb9Bg1kqPVckgGepS4KBxGSqw)

##  Linked repositories
* #### coreos_installation_iso:
    https://github.com/oshercc/coreos_installation_iso

    Image in charge of generating the Fedora-coreOs image used to install the host with the relevant ignition file.\
    Image is uploaded to deployed S3 under the name template "installer-image-<cluster-id>".
* #### ignition manifests and kubeconfig generate:

    https://github.com/oshercc/ignition-manifests-and-kubeconfig-generate

    Image in charge of generating the following installation files:
    * kubeconfig
    * bootstrap.ign
    * master.ign
    * worker.ign
    * metadata.json
    * kubeadmin-password

   Files are uploaded to deployed S3 under the name template "<cluster-id>/<filename>".
