# Third-Party CNI Support

Assisted Installer supports installing OpenShift clusters with third-party Container Network Interface (CNI) plugins
in addition to the built-in OVN-Kubernetes and OpenShiftSDN options.

## Supported Network Types

| `network_type` value | Description | Custom Manifests Required |
|----------------------|-------------|---------------------------|
| `OVNKubernetes` | Default CNI for OpenShift (recommended) | No |
| `OpenShiftSDN` | Legacy SDN (deprecated in OCP 4.15+) | No |
| `CiscoACI` | Cisco ACI CNI | **Yes** |
| `Cilium` | Isovalent Cilium CNI | **Yes** |
| `Calico` | Tigera Calico CNI | **Yes** |
| `None` | No CNI — user provides all networking manifests | **Yes** |

All third-party CNI options (`CiscoACI`, `Cilium`, `Calico`, `None`) require you to upload CNI manifests
before installation. The installer does not ship or generate manifests for third-party CNIs — you must obtain
them from the CNI vendor and upload them yourself.

For platform and version compatibility details per CNI, see
[Red Hat's CNI Certification Matrix](https://access.redhat.com/articles/5436171).

## Custom Manifest Requirement

When a third-party CNI is selected, the cluster validation `custom-manifests-requirements-satisfied` will block
installation until manifests are uploaded. The validation message will indicate:

> Custom manifests are required for: Cilium network type. Please upload manifests via the custom manifests API.

The installer checks for the *presence* of uploaded manifests but does not validate their CNI-specific content.
You are responsible for providing correct and version-compatible manifests from your CNI vendor.

## REST API Usage

Set `network_type` when creating or updating a cluster:

```bash
# Create a cluster with Cilium
curl -X POST "$ASSISTED_SERVICE_URL/api/assisted-install/v2/clusters" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-cluster",
    "openshift_version": "4.18",
    "pull_secret": "...",
    "network_type": "Cilium",
    ...
  }'

# Or update an existing cluster
curl -X PATCH "$ASSISTED_SERVICE_URL/api/assisted-install/v2/clusters/$CLUSTER_ID" \
  -H "Content-Type: application/json" \
  -d '{"network_type": "Cilium"}'
```

Then upload CNI manifests before installation:

```bash
curl -X POST "$ASSISTED_SERVICE_URL/api/assisted-install/v2/clusters/$CLUSTER_ID/manifests" \
  -H "Content-Type: application/json" \
  -d '{
    "file_name": "cilium-olm.yaml",
    "folder": "manifests",
    "content": "'$(base64 -w0 cilium-olm.yaml)'"
  }'
```

For full details on the custom manifests API, see
[Install Customization — Manifests](install-customization.md#manifests).

## Kube API Usage

In Kube API mode, set `spec.networking.networkType` on the `AgentClusterInstall` resource and provide
CNI manifests via `spec.manifestsConfigMapRefs`.

### 1. Create a ConfigMap with CNI manifests

Each key in the ConfigMap is a manifest filename, and the value is its content:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cilium-cni-manifests
  namespace: my-cluster-ns
data:
  cilium-olm.yaml: |
    apiVersion: operators.coreos.com/v1alpha1
    kind: Subscription
    metadata:
      name: cilium
      namespace: cilium
    spec:
      channel: "1.17"
      name: cilium-olm
      source: certified-operators
      sourceNamespace: openshift-marketplace
  cilium-config.yaml: |
    # Your Cilium configuration manifest here
    ...
```

### 2. Reference the ConfigMap and set networkType in AgentClusterInstall

```yaml
apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  name: my-cluster
  namespace: my-cluster-ns
spec:
  clusterDeploymentRef:
    name: my-cluster
  imageSetRef:
    name: openshift-v4.18.0
  networking:
    networkType: Cilium        # <-- third-party CNI
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    serviceNetwork:
    - 172.30.0.0/16
  provisionRequirements:
    controlPlaneAgents: 3
    workerAgents: 2
  manifestsConfigMapRefs:      # <-- required for third-party CNIs
  - name: cilium-cni-manifests
```

Multiple ConfigMaps can be referenced. Manifest names must be unique across all referenced ConfigMaps.

### Validation

The cluster will not proceed to installation until:

1. `networkType` is set to a valid value (`network-type-valid` validation)
2. Custom manifests are uploaded when using a third-party CNI (`custom-manifests-requirements-satisfied` validation)

Check validation status via the `AgentClusterInstall` conditions:

```bash
kubectl get agentclusterinstall my-cluster -n my-cluster-ns \
  -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.message}{"\n"}{end}'
```

### Differences from REST API

| Aspect | REST API | Kube API |
|--------|----------|----------|
| Field name | `network_type` | `spec.networking.networkType` |
| Manifest upload | POST to `/clusters/{id}/manifests` | ConfigMap referenced via `manifestsConfigMapRefs` |
| Default when unset | Auto-selected (OVN or SDN) | Auto-selected (OVN or SDN) |
| Validation feedback | HTTP 400 response | `Validated` condition on AgentClusterInstall |

The supported values and validation logic are identical in both modes.

## Notes

- **Default behavior is unchanged**: if `networkType` is omitted, OVN-Kubernetes is selected for OCP 4.12+
  and IPv6/SNO clusters; OpenShiftSDN for older IPv4 multi-node clusters.
- **OpenShiftSDN** is not available for Single Node OpenShift or IPv6 clusters.
- **CNI version** is determined by the manifests you provide, not by Assisted Installer.
  The installer validates CNI type compatibility but not CNI version.
- **Day 1 only**: CNI selection is an installation-time decision. Migrating between CNIs
  after installation is not supported through Assisted Installer.
