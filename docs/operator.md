# Operator build and deployment

## Prerequisites

1. operator-sdk <https://sdk.operatorframework.io/docs/installation/>
2. kustomize <https://github.com/kubernetes-sigs/kustomize/releases>
3. opm <https://github.com/operator-framework/operator-registry/releases>

## Building the operator bundle (optional)

For development and testing purposes it may be beneficial to build the operator
bundle and index images. If you don't __need__ to build it, just skip to
[Deploying the Operator With Hive](#deploying-the-operator-with-hive).

### Background

To generate the manifests and CSV in *./bundle* run  `make operator-bundle` .

The Makefile target first calls the `ocp-create-manifests` target. This target in turn calls `deploy-service-on-ocp-cluster` and `deploy-ui-on-ocp-cluster` while setting `APPLY_MANIFESTS=False` and `APPLY_NAMESPACE=False`. This causes the resource yamls to be created in *./build/assisted-installer/* but does not apply them.

The relevant resource yamls are then copied to *./config/assisted-service* where additional customizations are applied using **Kustomize**. The resulting yaml is then piped to operator-sdk, creating the manifests and CSVs in *./bundle/manifests*.

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
make operator-index-build
# Push index image used in catalog source
podman push $INDEX_IMAGE
```

## Deploying the operator on a k8s cluster

Kubernetes cluster needs to have Operator Lifecycle Manager. Latest realese can be found [here](https://github.com/operator-framework/operator-lifecycle-manager/releases)

```bash
kubectl apply -f https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.17.0/crds.yaml
kubectl apply -f https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.17.0/olm.yaml
```

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

Create Assisted Installer Operator CRDs as they are defined on community-operators hub.

```bash
for crd in `curl https://api.github.com/repos/operator-framework/community-operators/contents/community-operators/assisted-service-operator/0.0.2 | jq '.[].download_url'`
do
  kubectl apply -f ${crd}
done
```

Currently, due to a bug we have to specify the CSV namespace manually.

```bash
kubectl apply -n assisted-installer -f https://raw.githubusercontent.com/operator-framework/community-operators/master/community-operators/assisted-service-operator/0.0.2/assisted-service-operator.clusterserviceversion.yaml
```

An [OperatorGroup](https://docs.openshift.com/container-platform/4.7/rest_api/operatorhub_apis/operatorgroup-operators-coreos-com-v1.html) is mandatory for a CSV to function. We would like to generate everything in a single owned namespace named `assisted-installer`.

```bash
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
```

Eventually, you can verify that the assisted-installer operator was deployed by running

```bash
kubectl get csv assisted-service-operator.v0.0.2 -n assisted-installer
# NAME                               DISPLAY                     VERSION   REPLACES                           PHASE
# assisted-service-operator.v0.0.2   Assisted Service Operator   0.0.2     assisted-service-operator.v0.0.1   Failed

```

## Deploying the operator with Hive

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
EOF
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
an [OperatorGroup](https://docs.openshift.com/container-platform/4.7/rest_api/operatorhub_apis/operatorgroup-operators-coreos-com-v1.html) and a [Subscription](https://docs.openshift.com/container-platform/4.7/rest_api/operatorhub_apis/subscription-operators-coreos-com-v1alpha1.html) that are required by OLM.

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
  namespace: assisted-installer
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

For more details on how to specify the CR, see [AgentServiceConfig CRD](https://github.com/openshift/assisted-service/blob/master/internal/controller/config/crd/bases/agent-install.openshift.io_agentserviceconfigs.yaml).

## Subscription config

Subscription configs override any environment variables set in
the deployment specs and any values from ConfigMaps. They can be
used to configure the operator deployment.

Here is an example. By default, the operator bundle is configured
to use minimal-iso for ISO_IMAGE_TYPE. It can be reconfigured to
full-iso through the Subscription config.

``` bash
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
  source: assisted-service-manifests
  sourceNamespace: openshift-marketplace
  startingCSV: assisted-service-operator.v0.0.1
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
