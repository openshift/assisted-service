# assisted-service

## Debug Setup

To debug the application first set the `DEBUG_SERVICE` environment variable to any nonempty value

```shell
export DEBUG_SERVICE=true
```

The default remote debug port is 40000 but its configurable by setting the `DEBUG_SERVICE_PORT` env variable:

```shell
export DEBUG_SERVICE_PORT=8765
```

Build the image and push to your local k8s:

```shell
skipper make update-local-image
```

Note! When running this target in a DEBUG mode, \
it updates the existing assisted-service image with the latest assisted-service code, \
but if the Dockerfile itself has been changed you are responsible to update the image
before patching it.\
You have two options to update the image:

1. Pulling the latest assisted-service image:\
   This option is faster, but it pulls the latest master image

   - For a local minikube k8s:
     ```shell
     eval $(minikube docker-env) && docker pull IMAGE_NAME
     ```
   - For a local k3d k8s:
     ```shell
     docker pull IMAGE_NAME
     k3d image import IMAGE_NAME
     ```
   - For a local registry
     docker pull REMOTE_IMAGE_NAME
     docker tag REMOTE_IMAGE_NAME LOCAL_IMAGE_NAME
     docker push LOCAL_IMAGE_NAME

     ```

     ```

2. Build the image locally(Recommended)
   ```shell
   unset DEBUG_SERVICE
   skipper make update-local-image
   ```

Deploy the service to your local k8s:

```shell
skipper make deploy-all
```

Deploy the service for subsystem-test to your local k8s:

```shell
skipper make deploy-test
```

Build the image, push to your local k8s and restart the pods:

```shell
skipper make patch-service
```

Compile the code with debug information and patch the image:

```shell
skipper make update-debug-minimal
```
