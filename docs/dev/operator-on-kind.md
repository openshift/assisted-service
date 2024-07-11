# Deploying the operator on a kind cluster

The Infrastructure Operator can run on a kind cluster and will detect and alter its behavior to avoid relying on features only available in OpenShift.

## How to deploy

### Create a kind cluster

The cluster should be created using a kind config which adds port mappings for ingress on http (port 80)

For example:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
```

```sh
kind create cluster --config=<path/to/kind-config.yaml>
```

### Create required CRDs

Some CRDs are not included in the Infrastructure Operator CSV and must be installed separately.
For development these CRDs can be installed using copies in the assisted-service repo:

```sh
kubectl apply -f <assisted-service-repo>/hack/crds/
```

This creates the ClusterDeployment, ClusterImageSet, and BareMetalHost CRDs.

### Deploy the nginx ingress controller

An ingress controller is needed to be able to create ingress resources.
The nginx controller can be easily deployed with the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
```

### Deploy cert-manager

cert-manager is used to create and maintain certificates for use in https services and is required when running on non-OpenShift kubernetes.

```sh
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml
```

### Deploy the Infrastructure Operator and related CRDs from the local assisted-service repo

```sh
cd <assisted-service-repo>
kustomize build config/default/ | oc apply -f -
```

This will result in all the required CRDs and RBAC being created as well as an operator deployment.

### Create AgentServiceConfig

An agent service config can now be created which should include the kubernetes-specific fields for ingress hosts and ingress class name:

```yaml
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
  name: agent
spec:
  ingress:
    className: nginx
    assistedServiceHostname: assisted-service.example.com
    imageServiceHostname: image-service.example.com
  databaseStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 8Gi
  filesystemStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 8Gi
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  osImages:
  - openshiftVersion: "4.15"
    cpuArchitecture: "x86_64"
    url: "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.15/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"
    version: "415.92.202402130021-0"
```
