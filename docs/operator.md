# Operator build and deployment

## Prerequisites

1. operator-sdk <https://sdk.operatorframework.io/docs/installation/>
2. kustomize <https://github.com/kubernetes-sigs/kustomize/releases>
3. opm <https://github.com/operator-framework/operator-registry/releases>

## Building the operator bundle (optional)

For development and testing purposes it may be beneficial to build the operator
bundle and index images. If you don't __need__ to build it, just skip to
[Deploying the Operator](#deploying-the-operator).

### Background

To generate the manifests and CSV in ./bundle, "make operator-bundle" first calls the "ocp-create-manifests" target. This target in turn calls "deploy-service-on-ocp-cluster and deploy-ui-on-ocp-cluster" while setting APPLY_MANIFESTS=False and APPLY_NAMESPACE=False. This causes the resource yamls to be created in ./build/assisted-installer/ but does not apply them.

The relevant resource yamls are then copied to ./config/assisted-service where additional customizations are applied using Kustomize. The resulting yaml is then piped to operator-sdk, creating the manifests and CSVs in ./bundle/manifests.

More information about bundles: <https://sdk.operatorframework.io/docs/olm-integration/generation/>

### Create the bundle and index images

```bash
export ORG=quay.io/change-me
export BUNDLE_IMAGE=$ORG/assisted-service-operator-bundle:0.0.1
export INDEX_IMAGE=$ORG/assisted-service-index:0.0.1
# Build bundle image
make operator-bundle-build
# Push bundle image
make operator-bundle-update

# Create index image
opm index add --bundles $BUNDLE_IMAGE --tag $INDEX_IMAGE
# Push index image used in catalog source
podman push $INDEX_IMAGE
```

## Deploying the operator

The operator must be deployed to the assisted-installer namespace. Create the namespace.

```bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Namespace
metadata:
  name: assisted-installer
  labels:
    name: assisted-installer
EOF
```

Having the ClusterDeployment CRD installed is a prerequisite.
Install Hive, if it has not already been installed. Note the
startingCSV version, it may need to be updated to a more
recent version. See [version list](https://github.com/operator-framework/community-operators/tree/master/community-operators/hive-operator).

``` bash
cat <<EOF | kubectl create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: hive-operator
  namespace: openshift-operators
spec:
  channel: alpha
  installPlanApproval: Automatic
  name: hive-operator
  source: community-operators
  sourceNamespace: openshift-marketplace
  startingCSV: hive-operator.v1.0.19
```

Create a CatalogSource for the operator to appear in OperatorHub.
The CatalogSource spec image can be set to quay.io/ocpmetal/assisted-service-index:latest
if you want to pick up the latest merged change on master or to a
custom the index image.

``` bash
cat <<EOF | kubectl create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: assisted-service-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/ocpmetal/assisted-service-index:latest
EOF
```

It may take a few minutes for the operator to appear in OperatorHub.
Once it is in OperatorHub, the operator can be installed through the
console.

The operator can also be installed through the command line by creating
an OperatorGroup and Subscription.

``` bash
cat <<EOF | kubectl create -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
    name: assisted-installer-group
    namespace: assisted-installer
spec:
  targetNamespaces:
    - assisted-installer
EOF

cat <<EOF | kubectl create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: assisted-service-operator
  namespace: assisted-installer
spec:
  channel: alpha
  installPlanApproval: Automatic
  name: assisted-service-operator
  source: assisted-service-catalog
  sourceNamespace: openshift-marketplace
  startingCSV: assisted-service-operator.v0.0.1
EOF
```

## Deploying the operand

An Assisted Service Deployment is created by creating an
AgentServiceConfig CustomResource. Here is a basic example:

``` bash
cat <<EOF | kubectl create -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
  name: agent
spec:
  databaseStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  filesystemStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 20Gi
EOF
```

**NOTE Unsupported**
It is possible to specify a ConfigMap to be mounted into the assisted-service
container as environment variables by adding an
`"unsupported.agent-install.openshift.io/assisted-service-configmap"`
annotation to the `AgentServiceConfig` specifying the name of the configmap to be
used. This ConfigMap must exist in the namespace where the operator is
installed.

## Deploying the operand with mirror registry configuration

In case user wants to create an installation that uses mirror registry, the following must be executed:
1. Create and upload a ConfigMap that will include the following keys:
   ca-bundle.crt -  contents of the certificate for accessing the mirror registry. I may be a certificate bundle, but it is still one string
   registries.conf - contents of the registries.conf file that configures mapping to the mirror registry.

``` bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: mirror-registry-ca
  namespace: "assisted-installer"
  labels:
    app: assisted-service
data:
  ca-bundle.crt: |
    -----BEGIN CERTIFICATE-----
    certificate contents
    -----END CERTIFICATE-----

  registries.conf: |
    unqualified-search-registries = ["registry.access.redhat.com", "docker.io"]
    
    [[registry]]
       prefix = ""
       location = "quay.io/ocpmetal"
       mirror-by-digest-only = false
    
       [[registry.mirror]]
       location = "bm-cluster-1-hyper.e2e.bos.redhat.com:5000/ocpmetal"
EOF
```

   Note1: ConfigMap should be installed in the same namespace as the assisted-service-operator (ie. `assisted-installer`).
   
   Note2: registry.conf supplied should use "mirror-by-digest-only = false" mode

2. set the mirrorRegistryConfigmapName in the spec of AgentServiceConfig to the name of uploaded ConfigMap. Example:

``` bash
cat <<EOF | kubectl create -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
  name: agent
spec:
  databaseStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  filesystemStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 20Gi
  mirrorRegistryConfigmapName: mirrorRegistyMap
EOF
```

For more details on how to specify the CR, see [AgentServiceConfig CRD](https://github.com/openshift/assisted-service/blob/master/internal/controller/config/crd/bases/agent-install.openshift.io_agentserviceconfigs.yaml).

## Subscription config

Subscription configs override any environment variables set in
the deployment specs and any values from ConfigMaps. They can be
used to configure the operator deployment.

In the example below, we configure the image that will be used
when deploying the assisted-service by changing the value of `SERVICE_IMAGE`.

``` bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: assisted-service-operator
  namespace: assisted-installer
spec:
  channel: alpha
  installPlanApproval: Automatic
  name: assisted-service-operator
  source: assisted-service-manifests
  sourceNamespace: openshift-marketplace
  config:
    env:
      - name: SERVICE_IMAGE
        value: "my-custom-assisted-service-image"
EOF
```

## Useful Kustomize options

In [`config/default/kustomization.yaml`](https://github.com/openshift/assisted-service/blob/master/config/default/kustomization.yaml):

```
# Uncomment to set a mininum disk size allowed by the hardware validator.
# By default the minimum disk size allowed is 120GB. The patch sets the
# minimum disk size to 20GB.
#- assisted-service-configmap-patch-hw-validator-min-disk-size.yaml
```

```
# Uncomment to use a custom assisted-service image in the deployment
#- assisted-service-patch-image.yaml
```
