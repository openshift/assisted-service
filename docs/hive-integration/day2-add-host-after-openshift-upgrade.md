# Adding Nodes to an Upgraded Cluster (Day 2)

This page describes how to add a host to an OpenShift cluster that has already been installed (Day 2 operation) when the cluster has been upgraded to a new OpenShift version.

## Problem

Assisted Installer retains the original base version of the cluster from when it was first installed. When a cluster has been upgraded to a newer OpenShift version, it may become impossible to add new nodes because of the gap between the actual cluster version and the version that Assisted Installer assumes. This version mismatch can prevent the discovery image from being compatible with the upgraded cluster, making it impossible to add new hosts.

## Overview

When an OpenShift cluster has been upgraded to a new version after its initial installation, and you want to add new hosts to this cluster, you need to specify the current OS image version in the associated InfraEnv CR. This ensures that new hosts use the appropriate OS image (RHCOS) for the current cluster version.

## Solution

To add a host after an OpenShift upgrade, you need to update the existing InfraEnv CR (which was created with a `clusterRef`) by adding the `osImageVersion` field. This value must correspond to the current OpenShift version of the cluster.

The workflow is:
1. The InfraEnv CR was originally created with a `clusterRef` pointing to the cluster
2. After the cluster has been upgraded, update the InfraEnv CR to add the `osImageVersion` field
3. The `osImageVersion` field takes priority over the version of the cluster referenced by `clusterRef`

### Important Notes

- The `osImageVersion` field cannot be specified together with `clusterRef` when **creating** an InfraEnv CR
- However, it is **allowed to update** an existing InfraEnv CR that has a `clusterRef` by adding the `osImageVersion` field
- The `osImageVersion` value can be taken from the list of `openshift_version` present in the `OSImages` list of the `AgentServiceConfig`
- The version must be close enough to the current OS image version of the cluster
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
- `osImageVersion: "4.16"` is added to specify that the OS image must correspond to the current OS image version after the upgrade
- The `osImageVersion` field takes priority over the version of the cluster referenced by `clusterRef`

## Determining the OS image version

### Current version

To determine the current version of your OS image, you can connect to one of your nodes and run:

```bash
cat /etc/os-release
```

This returns the version along with other information. Example output:

```bash
NAME="Red Hat Enterprise Linux CoreOS"
ID="rhcos"
ID_LIKE="rhel fedora"
VERSION="418.94.202509100653-0"
VERSION_ID="4.18"
VARIANT="CoreOS"
VARIANT_ID=coreos
PLATFORM_ID="platform:el9"
PRETTY_NAME="Red Hat Enterprise Linux CoreOS 418.94.202509100653-0"
ANSI_COLOR="0;31"
CPE_NAME="cpe:/o:redhat:enterprise_linux:9::baseos::coreos"
HOME_URL="https://www.redhat.com/"
DOCUMENTATION_URL="https://docs.okd.io/latest/welcome/index.html"
BUG_REPORT_URL="https://access.redhat.com/labs/rhir/"
REDHAT_BUGZILLA_PRODUCT="OpenShift Container Platform"
REDHAT_BUGZILLA_PRODUCT_VERSION="4.18"
REDHAT_SUPPORT_PRODUCT="OpenShift Container Platform"
REDHAT_SUPPORT_PRODUCT_VERSION="4.18"
OPENSHIFT_VERSION="4.18"
RHEL_VERSION=9.4
OSTREE_VERSION="418.94.202509100653-0" # This should match the 'version' field in the AgentServiceConfig osImages entry
```

### List the available versions

To list the available versions, you can use the following command:
```bash
oc get agentserviceconfig agent -o jsonpath='{.spec.osImages}'
```

**Note:** The `osImages` list is configured by the administrator and may contain inconsistent entries. The `openshift_version` field acts as a key to identify each entry, but there are no guarantees it is consistent with the actual values in `url` or `version`. When selecting a value for `osImageVersion`, ensure that the associated `url` and `version` fields in that entry correspond to the correct OS image for your cluster.

More information about the adding/updating OS images when the version is not available can be found [here](./kube-api-select-ocp-versions.md#set-os-images-in-agentserviceconfig)

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
