# Running OpenShift Assisted Service Kubernetes

**Note:** To use build and use the current code as the image, run:

```bash
export USE_LOCAL_SERVICE=true
```

We currently support two k8s tools for deploying assisted-service - `minikube` and `kind`.
to choose between them specify `TARGET`, for example:

```bash
export TARGET=minikube
```

To deploy assisted-service and its components, run:

```bash
make deploy-on-k8s
```

With `minikube` You could use `minikube service assisted-installer-ui -n <namespace> --url` to expose the UI for external access. Run it in a seperate terminal. With `kind` it is exposed aoutomatically, you can access it in `http://127.0.0.1:8060`

**Note** - The scripts exist in this repository for `minikube` deployment still doesn't support complete installation or remote deployment. If you want to deploy assisted service on `k8s` to perform a complete installation or on remote server, consider using `kind` or using [assisted-test-infra](https://github.com/openshift/assisted-test-infra) scripts which can deploy assisted service on `minikube` to perform complete installation.

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
