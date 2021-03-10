# Operator build and deployment

## Prerequisites

1. operator-sdk <https://sdk.operatorframework.io/docs/installation/>
2. kustomize <https://github.com/kubernetes-sigs/kustomize/releases>
3. opm <https://github.com/operator-framework/operator-registry/releases>

## Building the operator

### Background

To generate the manifests and CSV in ./bundle, "make operator-bundle" first calls the "ocp-create-manifests" target. This target in turn calls "deploy-service-on-ocp-cluster and deploy-ui-on-ocp-cluster" while setting APPLY_MANIFESTS=False and APPLY_NAMESPACE=False. This causes the resource yamls to be created in ./build/assisted-installer/ but does not apply them.

The relevant resource yamls are then copied to ./config/assisted-service where additional customizations are applied using Kustomize. The resulting yaml is then piped to operator-sdk, creating the manifests and CSVs in ./bundle/manifests.

More information about bundles: <https://sdk.operatorframework.io/docs/olm-integration/generation/>

### Create the bundle and index images

```bash
export ORG=quay.io/ocpmetal
export BUNDLE_IMAGE $ORG/assisted-service-operator-bundle:latest
# Build bundle image
make operator-bundle-build
# Push bundle image
make operator-bundle-update

# Create index image
opm index add --bundles $BUNDLE_IMAGE --tag $ORG/assisted-service-index:0.0.1  --container-tool podman
# Push index image used in catalog source
podman push $ORG/assisted-service-index:0.0.1
```

## Deploying the operator through OperatorHub

The operator must be deployed to assisted-installer namespace. Create the namespace. 

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

Create two PVCs, one for postgres and another for s3 (temporary until filesystem implementation is available).

```bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  labels:
    app: postgres
  name: postgres-pv-claim
  namespace: assisted-installer
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
EOF

cat <<EOF | kubectl create -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  labels:
    app: scality
  name: scality-pv-claim
  namespace: assisted-installer
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
EOF
```

Create a catalog source for the operator to appear in OperatorHub.

``` bash
cat <<EOF | kubectl create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: assisted-service
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/ocpmetal/assisted-service-index:0.0.1
EOF
```

It may take a few minutes for the operator to appear in Operatorhub.

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
