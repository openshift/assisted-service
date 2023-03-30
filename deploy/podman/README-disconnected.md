# Deploy with podman in disconnected Environment

These instructions detail how to deploy the assisted installer service in a disconnected or air-gaped environment with no Internet connectivity available. By default the assisted installer assumes that Internet connectivity is available for pulling container images and boot ISO files. This document will cover how to setup and configure the assisted installer to run in a disconnected environment with no access to the Internet.

## Requirements

* A server with at least 8GB of RAM and 2+ vCPU
* A container registry to mirror an OpenShift release
* A web server to hold the Red Hat CoreOS (RHCOS) boot ISO

Make sure you have [podman](https://podman.io) version 3.3+ installed. If you must use an older version of podman, reference the [previous documentation and procedure](https://github.com/openshift/assisted-service/tree/v2.0.11#deploy-without-a-kubernetes-cluster) to avoid a [podman bug](https://github.com/containers/podman/issues/9609).

If you do not have a web server to host the ISO and a container registry available you can co-locate all these services on the same host that you run the Assisted Installer from. This host will be referred to as the "assisted installer host" in the rest of the document.

## Identify a Container Registry and Mirror Contents

You will need to have a container registry available in the disconnected environment to house all the container images that are required to complete an OpenShift install. See [Mirroring image for a disconnected installation](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html) or [Mirroring image for a disconnected installation using the oc-mirror plug-in](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-disconnected.html) for how to mirror the required images. 

If you do not have a container registry available you, instructions for installing a container registry and mirroring the contents into that registry can be found in [Creating a mirror registry with mirror registry for Red Hat OpenShift](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-creating-registry.html).

Once you have mirrored the OpenShift container images, as well as the [Operator Hub catalog](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html#olm-mirror-catalog_installing-mirroring-installation-images), you will need to mirror in additional container images that are used by the Assisted Installer as they are not mirrored by the standard mirror process. Once you have completed the mirroring of the OpenShift images as well as the Operator Catalog images run the following commands to mirror in the Assisted Installer containers.

### Mirror assisted-installer agents

You will need to mirror additional images to your internal mirror for the Assisted Installer to use. If you are using the _oc adm mirror_ process, you can run the following commands to mirror the additional images required.

```shell
$ podman pull quay.io/edge-infrastructure/assisted-installer-agent:latest
$ podman pull quay.io/edge-infrastructure/assisted-installer:latest
$ podman pull quay.io/edge-infrastructure/assisted-installer-controller:latest
$ podman tag quay.io/edge-infrastructure/assisted-installer-agent:latest <container image registry server:port>/edge-infrastructure/assisted-installer-agent:latest
$ podman tag quay.io/edge-infrastructure/assisted-installer:latest <container image registry server:port>/edge-infrastructure/assisted-installer:latest
$ podman tag quay.io/edge-infrastructure/assisted-installer-controller:latest <container image registry server:port>/edge-infrastructure/assisted-installer-controller:latest
$ podman push <container image registry server:port>/edge-infrastructure/assisted-installer-agent:latest
$ podman push <container image registry server:port>/edge-infrastructure/assisted-installer:latest
$ podman push <container image registry server:port>/edge-infrastructure/assisted-installer-controller:latest
```

If you are using the _oc mirror_ plugin, you can add the following section to the _imageset-config.yaml_ as documented in the [Creating the image set configuration](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-disconnected.html#oc-mirror-creating-image-set-config_installing-mirroring-disconnected) documentation.

```
additionalImages:
  - name: quay.io/edge-infrastructure/assisted-installer-agent:latest
  - name: quay.io/edge-infrastructure/assisted-installer:latest
  - name: quay.io/edge-infrastructure/assisted-installer-controller:latest
```

## Identify a Web Server for ISO mirroring

You will need to have a Web server available within your disconnected environment. The Assisted Installer requires a URL to retrieve the RHCOS ISO from on the fly. If you have an existing web server available in your disconnected environment you can use that to host this file. Otherwise, this section will detail some steps for a Fedora/RHEL box.

We need to host the RHCOS image required for booting. We will use NGINX to handle this for us. First get a copy of the RHCOS images and place them in /usr/share/nginx/html... 

```shell
$ sudo dnf install -y nginx
$ sudo mkdir -p /usr/share/nginx/html/pub/openshift-v4/dependencies/rhcos/4.10/4.10.16
$ cd /usr/share/nginx/html/pub/openshift-v4/dependencies/rhcos/4.10/4.10.16
$ sudo wget https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.10/4.10.16/rhcos-4.10.16-x86_64-live.x86_64.iso
$ sudo wget https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.10/4.10.16/sha256sum.txt
```

If you installed a web server on the assisted installer host, you will need to configure the firewall:

```shell
$ sudo firewall-cmd --permanent --add-port={80/tcp,443/tcp}
$ sudo firewall-cmd --reload
$ sudo systemctl enable nginx --now
```

## Running the Assisted Installer via podman

You will need to configure multiple files to properly use the Assisted Installer in a disconnected environment. You will need to gather up the following information before proceeding:

* signing cert for your container image registry
* host name for your container image registry
* hostname or IP address for the web server hosting the RHCOS ISO file (if not using the assisted installer host)

### Install the Assisted Installer Service

Start by creating a directory for the assistedInstaller and copy the `pod-persistent-disconnected.yml` and `configmap-disconnected.yml` files from this Git repo directory into the assistedInstaller directory:

```shell
$ mkdir ~/assistedInstaller
$ cp pod-persistent-disconnected.yml configmap-disconnected.yml ~/assistedInstaller
```

2. Create a registry.conf file and change all "\<container image registry server:port\>" entries to point to your assisted installer host name

```conf
unqualified-search-registries = ["registry.access.redhat.com", "docker.io"]
[[registry]]
   prefix = ""
   location = "quay.io/openshift-release-dev/ocp-release"
   mirror-by-digest-only = true
   [[registry.mirror]]
   location = "<container image registry server:port>/ocp4/openshift4"
[[registry]]
   prefix = ""
   location = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
   mirror-by-digest-only = true
   [[registry.mirror]]
   location = "<container image registry server:port>/ocp4/openshift4"
```

3. Edit `configmap-disconnected.yml` and update the following values to point to the IP address of the assisted installer host:

* IMAGE_SERVICE_BASE_URL - http://<IP address of assisted installer host>:8888
* SERVICE_BASE_URL - http://<IP address of assisted installer host>:8090

4. Edit the "RELEASE_IMAGES" section. Replace "quay.io/openshift-release-dev/ocp-release:4.10.22-x86_64" with the "Update Image" from the [Identify a Container Registry and Mirror Contents](#identify-a-container-registry-and-mirror-contents) steps.

'[{"openshift_version":"4.10","cpu_architecture":"x86_64","cpu_architectures":["x86_64"],"url":"quay.io/openshift-release-dev/ocp-release:4.10.22-x86_64","version":"4.10.22","default":true}]'

5. Edit the "OS_IMAGES" section, and be sure to update the host IP address to match your assisted install host:

'[{"openshift_version":"4.10","cpu_architecture":"x86_64","url":"http://172.16.35.23/pub/openshift-v4/dependencies/rhcos/4.10/4.10.16/rhcos-4.10.16-x86_64-live.x86_64.iso","version":"410.84.202205191234-0"}]'

6. Update the tls-ca-bundle.pem file with the contents of your container image registry rootCA. If you are using the OpenShift Mirror Registry you can find this in the `quay-rootCA/rootCA.pem` file in the root directory for the mirror registry install, or see the section [](#retrieving-tls-cert-from-mirror-registry) in the Appendix section of this document for the process to get your image registries certificate.

7. Update the registries.conf section with the contents of the registries.conf file you created in step 2. 

8. Update "AGENT_DOCKER_IMAGE" to point to your mirror copy of the assisted-installer-agent, for example "\<container image registry server:port\>/edge-infrastructure/assisted-installer-agent:latest"

9. Update "CONTROLLER_IMAGE" to point to your mirror copy of the assisted-installer-agent, for example "\<container image registry server:port\>/edge-infrastructure/assisted-installer-controller:latest"

10. Update "INSTALLER_IMAGE" to point to your mirror copy of the assisted-installer-agent, for example "\<container image registry server:port\>/edge-infrastructure/assisted-installer:latest"

11. Save the file

12. Run the AssistedInstaller 
```shell
$ podman play kube --configmap configmap-disconnected.yml pod-persistent-disconnected.yml
# startup can be slower when the VM is not connected to the internet
$ sudo firewall-cmd --permanent --add-port={8090/tcp,8080/tcp,8888/tcp}
$ sudo firewall-cmd --reload
```

The assisted installer is now available at http://<your host ip address>:8080

## Additional Configuration required for Cluster Deployment

### Ignition Config Override

We need to create a "ignition_config_override" that will allow the assisted install boot CD to use the container image mirror. You will need to have the registry.conf file that we created previously and you will need the certificate for the Image Registry if it is using a self-signed certificate. Create a file called `discovery-ignition.json` with the following contents:

discovery-ignition.json.template
```json
{"ignition_config_override": "{\"ignition\": {\"version\": \"3.1.0\"}, \"storage\": {\"files\": [{\"path\": \"/etc/containers/registries.conf\", \"mode\": 420, \"overwrite\": true, \"user\": { \"name\": \"root\"},\"contents\": {\"source\": \"data:text/plain;base64,BASE64_ENCODED_REGISTRY_CONF\"}}, {\"path\": \"/etc/pki/ca-trust/source/anchors/domain.crt\", \"mode\": 420, \"overwrite\": true, \"user\": { \"name\": \"root\"}, \"contents\": {\"source\":\"data:text/plain;base64,BASE64_ENCODED_LOCAL_REGISTRY_CRT\"}}]}}"}
```

Now create base64 encoded versions of the registry.conf you created in [Install Assisted Installer](#install-the-assisted-installer-service) and the root CA for the image registry and update our discovery-ignition.json file:

```shell
$ base64 -w0 registry.conf > registry.conf.b64
$ base64 -w0 /u01/quay/quay-rootCA/rootCA.pem  > quay.crt.b64
$ sed -i "s/BASE64_ENCODED_REGISTRY_CONF/$(cat registry.conf.b64)/" discovery-ignition.json
$ sed -i "s/BASE64_ENCODED_LOCAL_REGISTRY_CRT/$(cat quay.crt.b64)/" discovery-ignition.json
```

You will apply this file as a part of the [Build/Deploy a Cluster](#builddeploy-a-cluster) in the next section.

## Build/Deploy a Cluster

At this point, you can now follow standard Assisted Installer workflows to create a cluster. Keep in mind that you will need to add your mirror registry certificate to the install_config as and _additionalTrustBundle_ well as add the _imageContentSources_ that define your mirror registry. The process that you use to do this will vary based on if you are using the Assisted Installer Web UI, via all api calls, or using [aicli](https://github.com/karmab/aicli).

### Using the Assisted Installer GUI

You can use the Assisted Installer web UI to set up the initial cluster settings, but you will need to customize the cluster using the API in order to patch the ignition_config to use the internal mirror resources. Instructions for creating a cluster through the UI can be found in the [Assisted Installer User Guide](https://github.com/openshift/assisted-service/blob/master/docs/user-guide/README.md). Once the cluster is configured (but PRIOR to downloading the ISO file), follow the steps below [Patching the Cluster Definition](#patching-the-cluster-definition).

### Using AICLI

The [aicli](https://github.com/karmab/aicli) tool is a tool that can be used to deploy OpenShift clusters using the Assisted Installer service from the command line. This is a community supported tool, and is NOT a part of the OpenShift project. See the [How to use](https://github.com/karmab/aicli/blob/main/doc/index.md#how-to-use) section of the aicli tool on how to create a cluster using this tool. You will need to add the following additional sections in the parameters file in order to point to the mirror registry:

```
disconnected_url: <container image registry server:port>
installconfig:
    additionalTrustBundle: |
      -----BEGIN CERTIFICATE-----
      <contents of your signing certificate>
      -----END CERTIFICATE-----
    imageContentSources:
      <the values here are based on the output from the OpenShift mirror command>
ignition_config_override: <contents of the discovery-ignition.json file created earlier>
```

The values for the imageContentSources will be based on the output of the OpenShift mirror command that you used. See the "imageContentSourcePolicy.yaml" file that is generated after the successful creation of a mirror for the exact information that should go here.

### Using the API

#### Creating the Cluster

Follow the instructions [rest api getting started](https://github.com/openshift/assisted-service/blob/master/docs/user-guide/rest-api-getting-started.md) to create your cluster. 

#### Patching the Cluster Definition

Then use the instructions [Install Customization - Discovery Ignition](https://github.com/openshift/assisted-service/blob/master/docs/user-guide/install-customization.md#discovery-ignition) to apply the discovery-ignition.json file created in the [Ignition Config Override](#ignition-config-override)

Then use the instructions [Install Customization - Install Config](https://github.com/openshift/assisted-service/blob/master/docs/user-guide/install-customization.md#install-config) to apply the _additionalTrustBundle_ and _imageContentSources_ to the OpenShift install_config.yaml.

## Appendix

### Retrieving TLS cert from mirror registry

If you are using an self-signed certificate on your mirror registry, you will need to retrieve the signing cert so that it can be added to the assisted installer trust bundle as well as the discovery iso trust bundle and target cluster configuration. You can use the opensssl command to retrieve this file:

```shell
$ echo | openssl s_client -servername \
    <container image registry server:port> \
    -connect <container image registry server:port> 2>/dev/null | openssl x509 > tls-ca-bundle.pem
```

> **NOTE:** Be sure to update the \<container image registry server:port\> with your container image registry server name and port.

You will use the contents of the `tls-ca-bundle.pem` file in the [Install the Assisted Installer Service](#install-the-assisted-installer-service) as well as the [Build/Deploy a Cluster](#builddeploy-a-cluster) sections above.
