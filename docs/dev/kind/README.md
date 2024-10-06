# Development environment using kind
Using `kind` we can start a lightweight environment for **assisted-service** development.  
The **assisted-service** does not require all the components of Openshift Container Platform, so we can use this environment to save time and resources.  

## Requirements
We usually use the **podman** provider with `kind`, so it must be installed in our Operating System.  
Nowadays, **podman** is available in the packaging system of all major GNU/Linux distributions.  
Ir order to manage the kind cluster, we will also need to have [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl9) or [oc](https://docs.openshift.com/container-platform/4.8/cli_reference/openshift_cli/getting-started-cli.html) installed.

## Build the environment
This repository provides some tools to install `kind`, **assisted-service** and their dependencies.  
We can build developer environments by using `make` targets:
* infrastructure operator on kind:
```shell
make deploy-dev-infra
```
* developer environment for sylva:
```
make deploy-sylva
```
When `make` finishes, we will have an environment ready to work.  
We can prepare the kind `kubeconfig` credentials with:
```shell
kind get kubeconfig -n assisted-hub-cluster > ~/.kube/kind-assisted-service
```
To validate if kind is working, check the running pods:
```shell
$ export KUBECONFIG="${HOME}/.kube/kind-assisted-service"

$ kubectl get nodes
NAME                                    STATUS   ROLES           AGE     VERSION
assisted-hub-cluster-control-plane      Ready    control-plane   6m23s   v1.30.0

$ kubectl get pods -A
NAMESPACE                   NAME                                                            READY   STATUS    RESTARTS   AGE
assisted-installer          agentinstalladmission-67c949b84-7l2hr                           1/1     Running   0          6m48s
assisted-installer          agentinstalladmission-67c949b84-jchzp                           1/1     Running   0          6m48s
assisted-installer          assisted-image-service-0                                        1/1     Running   0          6m48s
assisted-installer          assisted-service-5b68cbdfc6-rg4nf                               2/2     Running   0          6m48s
baremetal-operator-system   baremetal-operator-controller-manager-5546cbc489-m2vvq          2/2     Running   0          7m10s
baremetal-operator-system   ironic-659b44f9c8-mghwb                                         3/3     Running   0          10m
cert-manager                cert-manager-cainjector-9d956987c-tzds4                         1/1     Running   0          11m
cert-manager                cert-manager-fdd97855b-2v8nh                                    1/1     Running   0          11m
cert-manager                cert-manager-webhook-9f799c7d7-t7cvr                            1/1     Running   0          11m
kube-system                 coredns-7db6d8ff4d-jmw84                                        1/1     Running   0          11m
kube-system                 coredns-7db6d8ff4d-rgr8r                                        1/1     Running   0          11m
kube-system                 etcd-assisted-hub-cluster-control-plane                         1/1     Running   0          11m
kube-system                 kindnet-mnhts                                                   1/1     Running   0          11m
kube-system                 kube-apiserver-assisted-hub-cluster-control-plane               1/1     Running   0          11m
kube-system                 kube-controller-manager-assisted-hub-cluster-control-plane      1/1     Running   0          11m
kube-system                 kube-proxy-cgv7b                                                1/1     Running   0          11m
kube-system                 kube-scheduler-assisted-hub-cluster-control-plane               1/1     Running   0          11m
local-path-storage          local-path-provisioner-988d74bc-2ww6k                           1/1     Running   0          11m
```

### Build time
The build time depends on the components and your own resources, such as computer hardware or Internet bandwidth. However, for reference, this should be the estimated time for known scenarios:

| environment | description | time |
| :---------: | :---------: | :--: |
| sylva | ironic + BMO + assisted | 15m |
| infra | infrastructure operator + assisted | 4m |

## Verify the `assisted-service`
Once all pods are in `Running` state, you can validate if the `assisted-service` is available by running this command:
```shell
# In the sylva environment the service uses https
curl -sk https://127.0.0.1:8090/api/assisted-install/v2/infra-envs
{"code":401,"message":"unauthenticated for invalid credentials"}

# In the infra environment the service uses http
curl -sk http://127.0.0.1:8090/api/assisted-install/v2/infra-envs
{"code":401,"message":"unauthenticated for invalid credentials"}
```
The error code 401 is OK, it means that the service is unable to authenticate you, but is running.  
You can disable the REST API authentication by following [these steps](#disable-the-rest-api-authentication).

## assisted-service configuration
In this environment, the configuration of the assisted-service is managed by a `ConfigMap`.  
It is located in the `assisted-installer` namespace and is called `assisted-service`.  
We can see or manage the options by editing it:
```shell
$ kubectl edit cm -n assisted-installer assisted-service
```
Remember to restart the **assisted-service** pods after the changes:
```shell
$ kubectl rollout restart deploy -n assisted-installer assisted-service
```

## Disable the REST API authentication
If we want to disable the REST API authentication of the **assisted-service** for our tests we can change the option `AUTH_TYPE` in the configuration and restart the deployment to apply the changes:
```shell
$ kubectl patch cm/assisted-service -n assisted-installer -p '{"data":{"AUTH_TYPE":"none"}}'
$ kubectl rollout restart deploy -n assisted-installer assisted-service
```
If we use the infrastructure operator, the assisted-service settings are managed using the ConfigMap `assisted-service-config`:
```shell
$ kubectl create cm assisted-service-config --from-literal=AUTH_TYPE=none -n assisted-installer
```

After this change we can send REST requests as explained [here](../../user-guide/rest-api-getting-started.md) without using the authentication bearer.  
Our development environment exposes the REST API in the port **8090**.

## Replace assisted-service image
During our development perhaps we would like to replace the current `assisted-service` image with our own custom image.  
To replace the **assisted-service** image that we are using we can do it by changing the `image` in the `assisted-service` deployment:
```shell
kubectl patch deploy/assisted-service \
  -n assisted-installer \
  --type merge \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"assisted-service","image":"<YOUR IMAGE>:<TAG>"}]}}}}'
```

## Delete the environment
To delete the environment simply execute the `kind.sh` script with the `delete` parameter:
```shell
<repository-dir>/hack/kind/kind.sh delete
```
> Warning: This action **REMOVES** all work done on the local cluster.

## Additional working environments
This document covers the setup of a **assisted-service** development scenario using previous work provided by the [sylva-poc project](https://github.com/jianzzha/sylva-poc), but it's not the only option.

It is also possible to configure an **assisted-service** development environment using the infrastructure operator. This setup is described in the document [operator-on-kind](../operator-on-kind.md).

And, if you only need to start up the assisted-service application, for small tests without integration with other operators, you can also do it using podman following the instructions written in [deploy/podman](../../../deploy/podman/README.md).

## Known issues
### Starting docker instead of podman
* Error:
```shell
ERROR: failed to list clusters: command "docker ps -a --filter label=io.x-k8s.kind.cluster --format '{{.Label "io.x-k8s.kind.cluster"}}'" failed with error: exit status 1
Command Output: permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: Get "http://%2Fvar%2Frun%2Fdocker.sock/v1.24/containers/json?all=1&filters=%7B%22label%22%3A%7B%22io.x-k8s.kind.cluster%22%3Atrue%7D%7D": dial unix /var/run/docker.sock: connect: permission denied
```
* Explanation:  
The scripts to create the environment have been prepared to work with `podman`. If you have `docker` installed, they may fail because for some operations docker requires root permissions.  
* Solution:
Set the variable `KIND_EXPERIMENTAL_PROVIDER` to **podman** and start the environment again.
```shell
export KIND_EXPERIMENTAL_PROVIDER=podman
```
