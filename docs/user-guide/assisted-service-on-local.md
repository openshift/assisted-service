# Running OpenShift Assisted Service (OAS) on local machine

For that we have some ways we could follow, let's take a look on some of them.

## Running OAS on Minikube

This is the easiest way to deploy OAS, there are some Make targets to do that, but firstly we need Minikube:

```shell
minikube start --driver=kvm2
skipper make deploy-all
skipper make deploy-ui
```

You could use `kubectl proxy` command to expose the Assisted Service UI for external access purpose

## Running OAS on Podman

See the [README](../../deploy/podman/) for details.

## Running OAS on vanilla K8s

work in progress...

## Running OAS on K3s

work in progress...

## Running OAS on KinD

work in progress...

## Deploying Monitoring service for OAS

This will allow you to deploy Prometheus and Grafana already integrated with Assisted installer:

```shell
# Step by step
make deploy-olm
make deploy-prometheus
make deploy-grafana

# Or just all-in
make deploy-monitoring
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

## Deploy by tag

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
