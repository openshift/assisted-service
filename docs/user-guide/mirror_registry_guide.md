
# Using the Per-Cluster Mirror Registry Feature in Assisted Service

This guide explains how to use the new mirror registry feature in the Assisted Service. This mirror registry configuration can be scoped to a cluster. It assumes familiarity with deploying new clusters using Assisted CRDs.
**Note:** This feature is currently supported when Assisted Service is running in KubeAPI mode.

## Overview

The process consists of two major steps:
1. **Initialize the Mirror Registry**
2. **Apply the CRDs with Updated Configurations**

## Step 1: Initialize the Mirror Registry

Follow these steps to set up your mirror registry. Ensure that the image versions in the mirror registry match those specified in the cluster configuration.

### 1. Set Environment Variables

To simplify and streamline the registry setup process, we define key environment variables. This makes it easier to adjust configurations without modifying the script directly.

```bash
export REGISTRY_IP="<your-registry-ip>"
export REGISTRY_PORT=5000
export REGISTRY_PATH="/root/.docker/registry"
export TLS_PATH="${REGISTRY_PATH}/tls"
export OCP_TAG="4.18.0-rc.4-x86_64"
export INSECURE=false
export REGISTRY_NAME="registry_<cluster-name>"
```

### 2. TLS Setup

**Why TLS is Needed:**
TLS (Transport Layer Security) is essential for ensuring secure communication between your cluster components and the mirror registry. It encrypts the data in transit, preventing unauthorized access and tampering.

**Secure Mode vs. Insecure Mode:**
- **Secure Mode:** This mode uses TLS certificates for encrypted communication. It is recommended for production environments to ensure secure and trusted connections.
- **Insecure Mode:** This mode skips TLS certificate validation, which might be useful for testing or internal environments. **Note:** Insecure mirror registry support is currently a work in progress (WIP).

If `INSECURE` is set to `false`, generate TLS certificates:

```bash
mkdir -p "$TLS_PATH"

openssl req -x509 -nodes -days 365 -newkey rsa:4096 \
  -keyout "$TLS_PATH/key.pem" \
  -out "$TLS_PATH/cert.pem" \
  -subj "/CN=$REGISTRY_IP" \
  -addext "subjectAltName=IP:$REGISTRY_IP"

cp "$TLS_PATH/cert.pem" /etc/pki/ca-trust/source/anchors/registry.crt
update-ca-trust
```

### 3. Configure the Registry

Create the registry configuration file based on the security setting:

**For Secure Setup:**
```yaml
version: 0.1
log:
  accesslog:
    disabled: true
  level: debug
  formatter: text

storage:
  filesystem:
    rootdirectory: /var/lib/registry

http:
  addr: :${REGISTRY_PORT}
  net: tcp
  relativeurls: false
  draintimeout: 60s
  tls:
    certificate: /certs/cert.pem
    key: /certs/key.pem
```

**For Insecure Setup:**
```yaml
version: 0.1
log:
  accesslog:
    disabled: true
  level: debug
  formatter: text

storage:
  filesystem:
    rootdirectory: /var/lib/registry

http:
  addr: :${REGISTRY_PORT}
  net: tcp
  relativeurls: false
  draintimeout: 60s
```

### 4. Start the Registry

Stop any existing registry container and start a new one:

```bash
# Make sure that there is no registry running with the same name
podman kill $REGISTRY_NAME || true
podman rm $REGISTRY_NAME || true

podman run -d --privileged \
  --network host \
  -v "$REGISTRY_PATH/config.yml:/etc/docker/registry/config.yml" \
  $( [[ "$INSECURE" != "true" ]] && echo "-v $TLS_PATH:/certs" ) \
  --restart=unless-stopped \
  --name $REGISTRY_NAME \
  registry:2
```

### 5. Verify the Registry

To ensure the registry is up and running, you can perform a simple check using `curl`.

1. **For Secure Mode:**

```bash
curl -k https://$REGISTRY_IP:$REGISTRY_PORT/v2/
```

2. **For Insecure Mode:**

```bash
curl http://$REGISTRY_IP:$REGISTRY_PORT/v2/
```

If the registry is working correctly, you will see an empty JSON response `{}`. If not, verify the registry logs and configurations.

### 6. Mirror Required Images

Ensure that the necessary images are mirrored to the registry. This process is divided into two parts:

#### 6.1 Mirror OpenShift Release Images

Mirroring OpenShift release images ensures that your cluster can access the required OpenShift Container Platform (OCP) images from the local mirror registry, especially useful in disconnected or restricted network environments. For detailed instructions, refer to the [OpenShift Documentation on Image Mirroring](https://docs.openshift.com/container-platform/4.16/installing/disconnected_install/installing-mirroring-installation-images.html).

```bash
oc adm release mirror \
  -a pull_secret.json \
  --from=quay.io/openshift-release-dev/ocp-release:$OCP_TAG \
  --to=$REGISTRY_IP:$REGISTRY_PORT/openshift-release-dev/ocp-release \
  --to-release-image=$REGISTRY_IP:$REGISTRY_PORT/openshift-release-dev/ocp-release:$OCP_TAG \
  --insecure=$INSECURE
```

- **Version Matching:** Ensure that the OpenShift version in the mirror registry matches the version specified in the image set configuration.


#### 6.2 Mirror Assisted Installer Images

These images are required for the discovery step:

```bash
oc image mirror \
  -a pull_secret.json \
  quay.io/edge-infrastructure/assisted-installer:latest \
  $REGISTRY_IP:$REGISTRY_PORT/edge-infrastructure/assisted-installer:latest \
  --insecure=$INSECURE

oc image mirror \
  -a pull_secret.json \
  quay.io/edge-infrastructure/assisted-installer-controller:latest \
  $REGISTRY_IP:$REGISTRY_PORT/edge-infrastructure/assisted-installer-controller:latest \
  --insecure=$INSECURE

oc image mirror \
  -a pull_secret.json \
  quay.io/edge-infrastructure/assisted-installer-agent:latest \
  $REGISTRY_IP:$REGISTRY_PORT/edge-infrastructure/assisted-installer-agent:latest \
  --insecure=$INSECURE
```

## Step 2: Apply the CRDs with Updated Configurations

Once the mirror registry is set up, apply the necessary CRDs to configure the cluster to use the mirrored images.

### 1. Apply the Mirror Registry ConfigMap

Create the `mirror_registry.yaml` file as follows:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mirror-registry-config
  namespace: <your-namespace>
data:
  registries.conf: |
    [[registry]]
      prefix = ""
      location = "quay.io/openshift-release-dev/ocp-release"
      [[registry.mirror]]
        location = "$REGISTRY_IP:$REGISTRY_PORT/openshift-release-dev/ocp-release"
        pull-from-mirror = "digest-only"

    [[registry]]
      prefix = ""
      location = "quay.io/edge-infrastructure"
      [[registry.mirror]]
        location = "$REGISTRY_IP:$REGISTRY_PORT/edge-infrastructure"
  ca-bundle.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
```

Apply the ConfigMap:

```bash
oc apply -f mirror_registry.yaml
```

### 2. Update and Apply CRDs

Ensure that your `AgentClusterInstall` and `InfraEnv` CRDs reference the mirror registry configuration.

**aci.yaml:**
```yaml
spec:
  mirrorRegistryRef:
    name: mirror-registry-config
    namespace: <your-namespace>
```

**infraenv.yaml:**
```yaml
spec:
  mirrorRegistryRef:
    name: mirror-registry-config
    namespace: <your-namespace>
```

Apply the CRDs:

```bash
oc apply -f aci.yaml
oc apply -f infraenv.yaml
```

### 3. Verify the Setup

Ensure that the images are being pulled from the mirror registry and that the cluster is deploying correctly.

## Per-Cluster Mirror Registry Configuration

With this feature, you have the flexibility to define a unique mirror registry for each cluster. This allows you to:

- Specify different registry names and ports for individual clusters.
- Run multiple mirror registries simultaneously without conflicts by using distinct container names defined by the `REGISTRY_NAME` variable.

For each cluster, simply adjust the `REGISTRY_IP`, `REGISTRY_PORT`, and `REGISTRY_NAME` environment variables to configure a dedicated mirror registry. This setup is particularly useful in multi-cluster environments where isolated registry configurations are required.

## Notes

- **Insecure Registry:** Insecure mirror registry support is currently a work in progress (WIP).
