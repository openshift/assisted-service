# How to run Assisted-service subsystem tests

There are two "flavors" of subsystem tests:

- subsystem tests for the service deployed in REST-API mode
- subsystem tests for the service deployed in kube-api mode

Assisted-service subsystem tests require that you first deploy the
assisted-service on a k8s cluster (e.g. minikube).

This document will explain how you can easily deploy the service in preperation
for subsystem tests, and also how to run the tests themselves.

The subsystem tests themselves are located in the
[subsystem](https://github.com/openshift/assisted-service/tree/master/subsystem)
directory, but they are launched via `make` targets in the Makefile at the root
of this repository.

## Service deployment

This section will show you how you can deploy the assisted installer to
a minikube cluster in preperation for subsystem tests.

### minikube

First we must prepare the minikube cluster -

```bash
# Optionally delete the existing minikube cluster:
# minikube delete

# Clean remains of any networks created by minikube
podman network rm minikube || true

# Start minikube with registry addon
minikube start --driver=podman --addons registry --addons dashboard --force

# Make the registry addon accessible locally:
nohup kubectl port-forward svc/registry 5000:80 -n kube-system &>/dev/null &
export LOCAL_SUBSYSTEM_REGISTRY=localhost:5000

echo "Waiting for registry to become ready..."
while ! curl --location $LOCAL_SUBSYSTEM_REGISTRY; do
    sleep 10
    echo "kubectl registry service tunnel at port 5000 is not available yet, retrying..."
    echo "If this persists, try running the kubectl port-forward command above without"
    echo "nohup, /dev/null redirection and the background job & operator and see if there"
    echo "are any errors"
done

# Make a tunnel to make minikube services reachable (the command will ask for root password):
nohup minikube tunnel &>/dev/null &
```

Now that the cluster is prepared, we can deploy the service -

To deploy the service in REST-API mode, run:

```bash
skipper make deploy-service-for-subsystem-test
```

To deploy the service in kube-api mode, run:

```bash
ENABLE_KUBE_API=true skipper make deploy-service-for-subsystem-test
skipper make enable-kube-api-for-subsystem
```

## Running the subsystem tests

To run the REST-API subsystem tests, run:

```bash
skipper make subsystem-test
```

To run the kube-api subsystem tests, run:

```bash
skipper make subsystem-test-kube-api
```

Optionally the following environment variables can be exported:

- `FOCUS="install_cluster"` - An optional flag used for [focused specs](https://onsi.github.io/ginkgo/#focused-specs) with regular expression.
- `SKIP="install_cluster"` - An optional flag to skip scopes with regular expressions.
- `VERBOSE=true` - An optional flag to print verbosed data.

## Update service for the subsystem tests

If you are making changes to the service's code and don't want to go through
the slow steps above once again, you can simply run this command instead:

```bash
skipper make patch-service
```

It will build and push a new image of the service to the container registry,
then trigger a rollout of the service Deployment.
