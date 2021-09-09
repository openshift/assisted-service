# How to run Assisted-service subsystem tests

Assisted-service subsystem tests require deploying the assisted-service on a k8s cluster together with DB and storage services.
The subsystem tests are located on the [subsystem](https://github.com/openshift/assisted-service/tree/master/subsystem) directory.

## Subsystem tests pre-configuration

* Run [minikube](https://minikube.sigs.k8s.io/docs/start/) on your system
* Enable [registry addon](https://minikube.sigs.k8s.io/docs/handbook/registry/) on your minikube:

```bash
minikube start
minikube addons enable registry
```

* Set `LOCAL_ASSISTED_ORG` to point to your local registry address

```bash
export LOCAL_ASSISTED_ORG=localhost:5000
```

* Redirect the local registry to the minikube registry:

```bash
nohup kubectl port-forward svc/registry 5000:80 -n kube-system &>/dev/null &
```

* Make a tunnel to make minikube services reachable (the command will ask for root password):

```bash
nohup minikube tunnel &>/dev/null &
```

* Deploy services:

```bash
skipper make deploy-test
```

## Run tests

```bash
skipper make [test|unit-test] [environment variables]
```

* `test` - Runs subsystem tests.
* `unit-test` - Runs unit tests.

Environment variables:

* `FOCUS="install_cluster"` - An optional flag used for [focused specs](https://onsi.github.io/ginkgo/#focused-specs) with regular expression.
* `SKIP="install_cluster"` - An optional flag to skip scopes with regular expressions.
* `TEST="./internal/host"` -  An optional flag used for testing a specific package.
* `VERBOSE=true` - An optional flag to print verbosed data.

## Update service for the subsystem tests

if you are making changes and don't want to deploy everything once again you can simply run this command:

```bash
skipper make patch-service
```

It will build and push a new image of the service to your Docker registry, then delete the service pod from minikube, the deployment will handle the update and pull the new image to start the service again.
