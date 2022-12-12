# Hive Integration - Selecting OpenShift Versions

As part of [Hive Integration](README.md), a means to add and select an OpenShift release version is required. In order to facilitate this functionality, the [ClusterImageSet](https://github.com/openshift/hive/blob/master/docs/using-hive.md#openshift-version) CRD is utilized for specifying a release image.

A useful use-case is an environment with mirrored releases, in which the release image is mirrored to a local registry.

To set a different RHCOS image for an OpenShift version: URL and version should be specified in [AgentServiceConfig](../../config/crd/bases/agent-install.openshift.io_agentserviceconfigs.yaml) CRD.

### [ClusterImageSet](https://github.com/openshift/hive/blob/master/apis/hive/v1/clusterimageset_types.go)

The ClusterImageSet is used for referencing to a OpenShift release image.
So available versions are represented in an Hive cluster by defined ClusterImageSet resources.
To use a specific release image, it should be defined in the [ClusterDeployment CRD](https://github.com/openshift/hive/blob/master/docs/using-hive.md#clusterdeployment) in either of the following manners:

- As a reference to the ClusterImageSet in `spec.provisioning.imageSetRef` property.
- Explicitly as a URL in `spec.provisioning.releaseImage` property.

An example of a ClusterImageSet:

```
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
  name: openshift-v4.7.0
spec:
  releaseImage: quay.io/openshift-release-dev/ocp-release:4.7.0-x86_64
```

## Usage

### Set OS images in AgentServiceConfig

A collection of RHCOS images can be defined within the AgentServiceConfig CRD as a mapping between a minor OpenShift version and image URL/version.

E.g.

```
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
spec:
  osImages:
    - openshiftVersion: 4.7
      url: https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.0/rhcos-4.7.0-x86_64-live.x86_64.iso
      version: 47.83.202102090044-0
      cpuArchitecture: "x86_64"
```

### Deploy ClusterImageSet

Deploy a ClusterImageSet with the requested release image.

E.g.

```
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
  name: openshift-v4.8.0
spec:
  releaseImage: quay.io/openshift-release-dev/ocp-release:4.8.0-fc.0-x86_64
```

### Define imageSetRef in the ClusterDeployment

The deployed ClusterImageSet should be referenced in the ClusterDeployment under `spec.provisioning.imageSetRef` property.

E.g.

```
apiVersion: hive.openshift.io/v1
kind: ClusterDeployment
spec:
  provisioning:
      imageSetRef:
        name: openshift-v4.8.0
```

## Flow

The flow of adding a new version is a follows:

- If a new RHCOS image is required:
  - Set `OSImage` in AgentServiceConfig under `spec.osImages`
  - `OSImage` should include:
    - `openshiftVersion` the OCP version in major.minor or major.minor.patch format.
    - `url` the RHCOS image (optionally a mirror).
    - `rootFSUrl` the RHCOS rootFS, used when creating minimal-iso configured Discovery ISOs (optionally a mirror).
    - `version` the RHOCS version.
    - `cpuArchitecture` the architecture supported by the image and rootFS.
  - Upon starting the service, the relevant host [boot-files](https://github.com/openshift/assisted-service/blob/3823630a0900c7f7a7113d7be4ff5a404a35186b/swagger.yaml#L16) are uploaded to S3/File storage.
- Deploy a ClusterImageSet with a new `releaseImage` URL.
  - The URL can be a mirror to a local registry.
- Deploy a ClusterDeployment, referencing to the ClusterImageSet under `spec.provisioning.imageSetRef`.
- Finally, a new cluster is created with the newly added [openshift_version](https://github.com/openshift/assisted-service/blob/3823630a0900c7f7a7113d7be4ff5a404a35186b/swagger.yaml#L4106).
