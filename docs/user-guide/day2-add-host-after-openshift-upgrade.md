# Adding Nodes to an Upgraded Cluster (Day 2)

This page describes how to add a host to an OpenShift cluster that has already been installed (Day 2 operation) when the cluster has been upgraded to a new OpenShift version.

## Problem

Assisted Installer retains the original base version of the cluster from when it was first installed. When a cluster has been upgraded to a significantly newer OpenShift version, it may become impossible to add new nodes because the gap between the actual cluster version and the version that Assisted Installer assumes becomes too large. This version mismatch can prevent the discovery image from being compatible with the upgraded cluster, making it impossible to add new hosts.

## Overview

When an OpenShift cluster has been upgraded to a new version after its initial installation, and you want to add new hosts to this cluster, you need to specify the current cluster version in the associated InfraEnv CR. This ensures that new hosts use the appropriate OS image (RHCOS) for the current cluster version.

## Solution

To add a host after an OpenShift upgrade, you need to update the existing InfraEnv CR (which was created or updated with a `clusterRef`) by adding the `osImageVersion` field. This value must correspond to the current OpenShift version of the cluster.

The workflow is:
1. The InfraEnv CR was originally created or updated with a `clusterRef` pointing to the cluster
2. After the cluster has been upgraded, update the InfraEnv CR to add the `osImageVersion` field
3. The `osImageVersion` field takes priority over the version of the cluster referenced by `clusterRef`

### Important Notes

- The `osImageVersion` field cannot be specified together with `clusterRef` when **creating** an InfraEnv CR
- However, it is **allowed to update** an existing InfraEnv CR that has a `clusterRef` by adding the `osImageVersion` field
- The `osImageVersion` value must correspond to an OpenShift version present in the `OSImages` list of the `AgentServiceConfig`
- The version must match (or be close enough) the current OpenShift cluster version
- When both `clusterRef` and `osImageVersion` are present, `osImageVersion` takes priority over the version of the cluster referenced by `clusterRef`

## Example

The following example shows how to update an existing InfraEnv CR to add a host to a cluster that has been upgraded to OpenShift 4.16.

### Step 1: Original InfraEnv CR (created with clusterRef)

The InfraEnv CR was originally created with a `clusterRef`:

```yaml
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: myinfraenv-day2
  namespace: openshift-assisted-installer
spec:
  clusterRef:
    name: my-cluster
    namespace: openshift-assisted-installer
  pullSecretRef:
    name: pull-secret
  sshAuthorizedKey: 'ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB...'
```

### Step 2: Update the InfraEnv CR to add osImageVersion

After the cluster has been upgraded to OpenShift 4.16, update the InfraEnv CR by adding the `osImageVersion` field:

```yaml
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: myinfraenv-day2
  namespace: openshift-assisted-installer
spec:
  clusterRef:
    name: my-cluster
    namespace: openshift-assisted-installer
  osImageVersion: "4.16" # Added field
  pullSecretRef:
    name: pull-secret
  sshAuthorizedKey: 'ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB...'
```

In this example:
- The InfraEnv CR retains its original `clusterRef` to the cluster
- `osImageVersion: "4.16"` is added to specify that the OS image must correspond to OpenShift 4.16, which is the current cluster version after the upgrade
- The `osImageVersion` field takes priority over the version of the cluster referenced by `clusterRef`

## Determining the cluster version

To determine the current version of your OpenShift cluster, you can use the following command:

```bash
oc get clusterversion version -o jsonpath='{.status.desired.version}'
```

To list the available version, you can use the following command:
```bash
oc get agentserviceconfig agent -o jsonpath='{.spec.osImages}'
```

## Validating the change

Before adding a new node, you can validate that your change has been taken into account by checking that the ISO download URL in the InfraEnv status has been updated. The `isoDownloadURL` field in the status reflects the discovery image that corresponds to the specified `osImageVersion`.

To check the ISO download URL:

```bash
oc get infraenv <infraenv-name> -n <namespace> -o jsonpath='{.status.isoDownloadURL}'
```

Or to view the full status:

```bash
oc get infraenv <infraenv-name> -n <namespace> -o yaml
```

After updating the InfraEnv CR with `osImageVersion`, wait for the reconciliation to complete and verify that the `isoDownloadURL` in the status has been updated to reflect the new version. This confirms that the system has generated the appropriate discovery image for the specified version.

## Next steps

Once the InfraEnv CR is updated with the `osImageVersion` field correctly configured and you have validated that the ISO download URL has been updated:

1. The system will generate the appropriate discovery image for the specified version
2. You can boot the new hosts with this image
3. The hosts will be discovered and can be added to the existing cluster
