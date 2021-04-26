# assisted-service

## Debug Setup

To debug the application first set the `DEBUG` environment variable to any nonempty value

```shell
export DEBUG=true
```

The default remote debug port is 40000 but its configurable by setting the `DEBUG_PORT` env variable:

```shell
export DEBUG_PORT=8765
```

Compile the code with debug information, patch the image and push to your local k8s:

```shell
skipper make _update-local-image
```
Note! This target updates the existing assisted-service image with the latest assisted-service, but you are responsible to update the image
if the Dockerfile has been changed before patching it.\
You have two options to update the image:
1. Pulling the latest assisted-service image:\
   This option is faster, but it pulls the latest master image
    * For a local minikube k8s:
        ```shell
        eval $(minikube docker-env) && docker pull IMAGE_NAME
        ```
    * For a local k3d k8s:
        ```shell
        docker pull IMAGE_NAME
        k3d image import IMAGE_NAME
        ```
2. Build the image locally(Recommended)
    ```shell
    unset DEBUG
    skipper make _update-local-image
    ```

Deploy the service to your local k8s:

```shell
skipper make deploy-all
```

Compile the code with debug information and patch the image:

```shell
skipper make update-debug-minimal
```







