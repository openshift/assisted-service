# Operator build and deployment

## Prerequisites

1. operator-sdk <https://sdk.operatorframework.io/docs/installation/>
2. kustomize <https://github.com/kubernetes-sigs/kustomize/releases>
3. opm <https://github.com/operator-framework/operator-registry/releases>

## Building the operator bundle (optional)

For development and testing purposes it may be beneficial to build the operator
bundle and index images. If you don't __need__ to build it, just skip to
[Deploying the Operator](#deploying-the-operator).

Build the bundle:

```
export BUNDLE_IMAGE=quay.io/${QUAY_NAMESPACE}/assisted-service-operator-bundle:${TAG}
skipper make operator-bundle-build
```

**NOTE**

It is possible to run `make` command without using Skipper. In such a scenario all the required dependencies can be installed using the [setup_env.sh script](../hack/setup_env.sh).

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
Install Hive, if it has not already been installed.

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
EOF
```

Deploy the operator using the operator-sdk:

```bash
operator-sdk run bundle \
  --namespace assisted-installer \
  ${BUNDLE_IMAGE:-quay.io/edge-infrastructure/assisted-service-operator-bundle:latest}
```

Now you should see the `infrastructure-operator` deployment running in the
`assisted-installer` namespace.

**NOTE**

```bash
operator-sdk cleanup --namespace assisted-installer assisted-service-operator
```

Is an effective way to remove the operator when installed via `operator-sdk run`.

## Creating an AgentServiceConfig Resource

The Assisted Service is deployed by creating an AgentServiceConfig.
At a minimum, you must specify the `databaseStorage` and `filesystemStorage` to
be used.


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
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
EOF
```

## Configuring the Assisted Service Deployment

### Via Subscription

The operator subscription can be used to configure the images used in the
assisted-service deployment and the installer + controller + agent images used by
the assisted-service.

``` bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: assisted-service-operator
  namespace: assisted-installer
spec:
  config:
    env:
      - name: SERVICE_IMAGE
        value: ${SERVICE_IMAGE}
      - name: DATABASE_IMAGE
        value: ${DATABASE_IMAGE}
      - name: AGENT_IMAGE
        value: ${AGENT_IMAGE}
      - name: CONTROLLER_IMAGE
        value: ${CONTROLLER_IMAGE}
      - name: INSTALLER_IMAGE
        value: ${INSTALLER_IMAGE}
EOF
```

**NOTE**

The default channel for the assisted-service-operator package, here and in
[community-operators](https://github.com/operator-framework/community-operators/tree/master/community-operators/assisted-service-operator),
is `"alpha"` so we do not include it in the Subscription.

### Available Operator System Images

Locations of OS Images to be used when generating the discovery ISOs for
different OpenShift versions can be specified via the `osImages` field on the
AgentServiceConfig.

```
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
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  osImages:
    - openshiftVersion: "4.6"
      version: "46.82.202012051820-0"
      url: "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso"
      cpuArchitecture: "x86_64"
    - openshiftVersion: "4.7"
      version: "47.83.202103251640-0"
      url: "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-4.7.7-x86_64-live.x86_64.iso"
      cpuArchitecture: "x86_64"
    - openshiftVersion: "4.8"
      version: "47.83.202103251640-0"
      url: "https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.7/rhcos-4.7.7-x86_64-live.x86_64.iso"
      cpuArchitecture: "x86_64"
```

### Specifying Environmental Variables via ConfigMap

It is possible to specify a ConfigMap to be mounted into the assisted-service
container as environment variables by adding an
`"unsupported.agent-install.openshift.io/assisted-service-configmap"`
annotation to the `AgentServiceConfig` specifying the name of the configmap to be
used. This ConfigMap must exist in the namespace where the operator is
installed.

Simply create a ConfigMap in the `assisted-installer` namespace:

``` bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-assisted-service-config
  namespace: assisted-installer
data:
  LOG_LEVEL: "debug"
EOF
```

Add the annotation to the AgentServiceConfig:

```bash
oc annotate --overwrite AgentServiceConfig agent unsupported.agent-install.openshift.io/assisted-service-configmap=my-assisted-service-config
```

**NOTE**

After modifying content of the ConfigMap a new rollout of the Deployment has to be forced. This can be done with

```bash
oc rollout restart deployment/assisted-service -n assisted-installer
```

### Toggle TLS Check on Assisted Image Service

It is possible to toggle TLS checking from the Assisted Image Service by using the annotation `"unsupported.agent-install.openshift.io/assisted-image-service-skip-verify-tls"` on the AgentServiceConfig CR. By default, this is set to `false`, meaning all TLS connections are verified. When this annotation is set to `true`, then the Assisted Image Service skips verifying TLS connections.

It affects the following services: downloading ISO images from a secure server.

Add the annotation to the AgentServiceConfig:

```bash
oc annotate --overwrite AgentServiceConfig agent unsupported.agent-install.openshift.io/assisted-image-service-skip-verify-tls=true
```

### Mirror Registry Configuration

A ConfigMap can be used to configure assisted service to create installations using mirrored content. The ConfigMap contains two keys:

- *ca-bundle.crt* - This key contains the contents of the certificate for accessing the mirror registry, if necessary. It may be a certificate bundle and is defined as a single string.
- *registries.conf* - This key contains the contents of the registries.conf file that configures mappings to the mirror registry.

The mirror registry configuration changes the discovery image's ignition config, with *ca-bundle.crt* written out to */etc/pki/ca-trust/source/anchors/domain.crt* and with *registries.conf* written out to */etc/containers/registries.conf*. The configuration also changes the *install-config.yaml* file used to install a new cluster, with the contents of *ca-bundle.crt* added to *additionalTrustBundle* and with the registries defined *registries.conf* added to *imageContentSources* as mirrors.

1. To configure the mirror registry, first create and upload the ConfigMap containing the *ca-bundle.crt* and *registries.conf* keys.

``` bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: mirror-registry-config-map
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
       location = "quay.io/edge-infrastructure"
       mirror-by-digest-only = false

       [[registry.mirror]]
       location = "mirror1.registry.corp.com:5000/edge-infrastructure"
EOF
```

**NOTE**

The ConfigMap should be installed in the same namespace as the infrastructure-operator (ie. `assisted-installer`).

Registries listed in the `unqualified-search-registries` will be automatically added to an authentication ignore list (`PUBLIC_CONTAINER_REGISTRIES` environment variable) and will not be required by `assisted-service` when it is validating the pull secret.

Registries defined in the *registries.conf* file should use "mirror-by-digest-only = false" mode.

Registries defined in the *registries.conf* must be scoped by repository and not by registry. In the above example, *quay.io/edge-infrastructure* and *mirror1.registry.corp.com:5000/edge-infrastructure* are both scoped by the *edge-infrastructure* repository and this is a valid configuration. In the example below, removing the repository *edge-infrastructure* from location is an invalid configuration and will not pass openshift-installer validation:

``` 
# invalid configuration
    [[registry]]
       prefix = ""
       location = "quay.io"
       mirror-by-digest-only = false

       [[registry.mirror]]
       location = "mirror1.registry.corp.com:5000"
```

2. Then set the mirrorRegistryRef in the spec of AgentServiceConfig to the name of uploaded ConfigMap. Example:

``` bash
cat <<EOF | kubectl apply -f -
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
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  mirrorRegistryRef:
    name: mirror-registry-config-map
EOF
```

For more details on how to specify the CR, see [AgentServiceConfig CRD](https://github.com/openshift/assisted-service/blob/master/config/crd/bases/agent-install.openshift.io_agentserviceconfigs.yaml).

### Image Registries Without Authentication

`assisted-service` validates the [pull secret](hive-integration/kube-api-getting-started.md#2-create-a-pull-secret) provided for spoke cluster installation by making sure it contains the authentication information for every image registry used for installation. 

Registries that don't require authentication can be listed under `spec.unauthenticatedRegistries` in the `AgentServiceConfig` resource. Any registry on this list is not required to have an entry in the pull secret validated by the `assisted-service`.

``` bash
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
  name: agent
spec:
  unauthenticatedRegistries:
  - example.registry.com
  - example.registry2.com
  ...
```

**NOTE:**

[Mirror registries](#mirror-registry-configuration) will automatically be added to the ignore list and does need not to be added under `spec.unauthenticatedRegistries`.

Specifying the `PUBLIC_CONTAINER_REGISTRIES` environment variable in the [ConfigMap override](#specifying-environmental-variables-via-configmap) is still supported and will completely overwrite the list to whatever is in the override.
