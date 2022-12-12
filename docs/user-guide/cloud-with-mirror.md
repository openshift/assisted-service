# Saas + on premise registry

It is possible to use the Assisted Installer service hosted at [console.redhat.com](https://console.redhat.com) while also leveraging an on premise mirror registry. These instructions build on existing documentation, and can be used as supplemental instructions to support an on premise container registry mirror.

## Caveats

If you choose to use the Assisted Installer service hosted at [console.redhat.com](https://console.redhat.com) for your cluster installs, the machines in the cluster will STILL require access to _console.redhat.com_ either directly or via a proxy configuration. In addition, as of right now, clusters built leveraging this process will still need the ability to pull the following images directly from quay.io:

- quay.io/edge-infrastructure/assisted-installer-agent:latest
- quay.io/edge-infrastructure/assisted-installer:latest
- quay.io/edge-infrastructure/assisted-installer-controller:latest
- registry.redhat.io/rhai-tech-preview/assisted-installer-agent-rhel8:v1.0.0-195

The Assisted Installer service hosted at [console.redhat.com](https://console.redhat.com) does not support using mirrored instances of the above container images at this time.

If you use the "minimal" ISO image to boot your machines, each host will pull an ISO image from [mirror.openshift.com](https://mirror.openshift.com) in order to complete the initial boot process. It is suggested to use the full image to keep this from occurring.

## Requirements

- A container registry to mirror an OpenShift release
- curl
- jq
- sed
- base64

## Identify a Container Registry and Mirror Contents

You will need to have a container registry available in the disconnected environment to house all the container images that are required to complete an OpenShift install. See [Mirroring image for a disconnected installation](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html) or [Mirroring image for a disconnected installation using the oc-mirror plug-in](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-disconnected.html) for how to mirror the required images.

If you do not have a container registry available you, instructions for installing a container registry and mirroring the contents into that registry can be found in [Creating a mirror registry with mirror registry for Red Hat OpenShift](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-creating-registry.html).

Once you have mirrored the OpenShift container images, and the [Operator Hub catalog](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html#olm-mirror-catalog_installing-mirroring-installation-images) you can move onto the next steps.

> **NOTE**: You must ensure that you have mirrored a copy of OCP that _EXACTLY_ matches the version that you are installing from the Assisted Installer service hosted at [console.redhat.com](https://console.redhat.com). eg. If you are installing OpenShift 4.11.5 from the Assisted Installer, you must have previously mirrored version 4.11.5 to your local registry.

## Environment Setup

### User Authentication

You will need to authenticate to the SaaS service. The process for authenticating is documented in the [Authentication](../cloud.md#authentication) section of the `cloud.md` file located in this directory.

> **NOTE:** The JWT token is **valid for 15 minutes**, you can refresh the token by re-running the process referenced above.

### Get the cert from your registry

If you are using an self-signed certificate on your mirror registry, you will need to retrieve the signing cert so that it can be added to the assisted installer trust bundle as well as the discovery iso trust bundle and target cluster configuration. You can use the opensssl command to retrieve this file:

```
$ echo | openssl s_client -servername \
    <container image registry server:port> \
    -connect <container image registry server:port> 2>/dev/null | openssl x509 > tls-ca-bundle-registry.pem
```

NOTE: Be sure to update the <container image registry server:port> with your container image registry server name and port.

### Create a cluster in the Assisted Installer UI

Create a base cluster definition in the [Assisted Installer service](https://console.redhat.com/openshift/assisted-installer/clusters/~new) hosted at [console.redhat.com](https://console.redhat.com). Select the appropriate options until you get to the "Add Hosts" step, but **DO NOT** download the ISO image yet. We will need to make modifications to the cluster before downloading the ISO file. Once you have gotten to this step record the UUID that is generated. This can be found in the URL such as:

ht<area>tps://console.redhat.com/openshift/assisted-installer/clusters/**6ad83d29-5d18-4999-8e01-88484d1d2122** where **6ad83d29-5d18-4999-8e01-88484d1d2122** is the UUID.

Set the UUID variable for later use:

```
$ CLUSTER_UUID=<your cluster UUID>
```

Test to ensure that you are properly authenticated to the hosted service:

```
$ curl https://api.openshift.com/api/assisted-install/v2/clusters -H "Authorization: Bearer ${JWT_TOKEN}" | jq -r '.[].id'
6ad83d29-5d18-4999-8e01-88484d1d2122
```

You should get back a list of one or more UUIDs, one of which is the UUID for the cluster you just created.

### Set the INFRA_ENV_ID Variable

With the CLUSTER_UUID set, we now can set our INFRA_ENV_ID by running the following command:

```
$ curl https://api.openshift.com/api/assisted-install/v2/infra-envs?cluster_id=$CLUSTER_UUID -H "Authorization: Bearer ${JWT_TOKEN}" | jq -r '.[].id'
ce634417-a802-4f1f-bf62-bf502b5c98ee
$ INFRA_ENV_ID=ce634417-a802-4f1f-bf62-bf502b5c98ee
```

With our base environment variables set up, we can move on to making the required configuration changes to use your internal registry mirror.

## Create Discovery Ignition Patch File

The discovery ignition is used to make changes to the CoreOS live iso image which runs before we actually write anything to the target disk. An example use case would be configuring a separate container registry to pull the assisted-installer-agent image from.

The discovery ignition must use version 3.1.0 regardless of the version of the cluster that will eventually be created.

First we need to create a new `registries.conf` file which will tell the AI Installer to pull from your local mirror:

```conf
unqualified-search-registries = ["registry.access.redhat.com", "docker.io"]
[[registry]]
   prefix = ""
   location = "quay.io/openshift-release-dev/ocp-release"
   mirror-by-digest-only = true
   [[registry.mirror]]
   location = "<mirror_host_name>:<mirror_host_port>/ocp4/openshift4"
[[registry]]
   prefix = ""
   location = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
   mirror-by-digest-only = true
   [[registry.mirror]]
   location = "<mirror_host_name>:<mirror_host_port>/ocp4/openshift4"
```

> **NOTE:** Make sure to update the "mirror*host_name" and "mirror_host_port" to reflect \_your* local registry mirror.

Next we will create a template file that will be used to create our ignition patch file:

discovery-ignition.json.template

```json
{
  "ignition_config_override": "{\"ignition\": {\"version\": \"3.1.0\"}, \"storage\": {\"files\": [{\"path\": \"/etc/containers/registries.conf\", \"mode\": 420, \"overwrite\": true, \"user\": { \"name\": \"root\"},\"contents\": {\"source\": \"data:text/plain;base64,BASE64_ENCODED_REGISTRY_CONF\"}}, {\"path\": \"/etc/pki/ca-trust/source/anchors/domain.crt\", \"mode\": 420, \"overwrite\": true, \"user\": { \"name\": \"root\"}, \"contents\": {\"source\":\"data:text/plain;base64,BASE64_ENCODED_LOCAL_REGISTRY_CRT\"}}]}}"
}
```

Next create base64 encoded versions of the registry.conf and tls-ca-bundle and update our discovery-ignition.json file:

```shell
$ base64 -w0 registries.conf > registries.conf.b64
$ base64 -w0 tls-ca-bundle-registry.pem  > tls-ca-bundle-registry.pem.b64
$ cp discovery-ignition.json.template discovery-ignition.json
$ sed -i "s/BASE64_ENCODED_REGISTRY_CONF/$(cat registries.conf.b64)/" discovery-ignition.json
$ sed -i "s/BASE64_ENCODED_LOCAL_REGISTRY_CRT/$(cat tls-ca-bundle-registry.pem.b64)/" discovery-ignition.json
```

Apply the patch to the Assisted Installer:

```shell
$ curl \
    --header "Content-Type: application/json" \
    --header "Authorization: Bearer $JWT_TOKEN" \
    --request PATCH \
    --data @discovery-ignition.json \
"https://api.openshift.com/api/assisted-install/v2/infra-envs/$INFRA_ENV_ID"
```

Finally we can validate that the patch was properly applied:

```shell
curl -s -X GET \
  --header "Content-Type: application/json" \
  -H "Authorization: Bearer $JWT_TOKEN" \
  "https://api.openshift.com/api/assisted-install/v2/infra-envs/$INFRA_ENV_ID" \
  | jq -r
```

At this point our discovery ISO image is ready to be downloaded. You can follow published procedures to download the ISO and boot your machines into the discovery phase.

## Patch your install config

We need to add three things to our hosted install config, the cert from your internal registry, and updated secrets file and an image override section.

### Update your pull_secret.json with additional auth required to log into your internal mirror

Start by downloading a copy of your pull secret from the [Red Hat OpenSHift Cluster Manager](https://console.redhat.com/openshift/install/pull-secret) and save in your home directory. We will be updating this file with additional authentication in the next step.

If you haven't already set up to trust the certificate from your registry on your workstation follow these steps:

```
$ sudo cp tls-ca-bundle-registry.pem /etc/pki/ca-trust/source/anchors/tls-ca-bundle-registry.pem
$ sudo update-ca-trust extract
```

Now log into your internal registry using podman and be sure to point to the "pull-secret.json" file you downloaded earlier:

```
$ podman login --authfile=~/pull-secret.json <mirror_host_name>:<mirror_host_port>
```

### Apply the patch to the Cluster Definition

You should have completed the three previous steps, creating a `tls-ca-bundle-registry.pem`, `pull-secret.json` and `ics.json` file in your local directory before proceeding with the next steps. With those files created we can apply them to our cluster definition. Start by creating the patch file:

```bash
$ install_config_patch=$(mktemp)
$ jq -n --arg BUNDLE "$(cat tls-ca-bundle-registry.pem)" --arg SECRET "$(cat pull-secret.json)" \
'{
    "pullSecret": $SECRET,
    "additionalTrustBundle": $BUNDLE,
    "imageContentSources": [
        {
            "mirrors": [
                "<mirror_host_name>:<mirror_host_port>/ocp4/openshift4"
            ],
        "source": "quay.io/openshift-release-dev/ocp-release"
        },
        {
            "mirrors": [
                "<mirror_host_name>:<mirror_host_port>/ocp4/openshift4"
            ],
            "source": "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
        }
    ]
}| tojson' > $install_config_patch
```

> **NOTE:** Make sure to update the "mirror_host_name" and "mirror_host_port" to reflect your local registry mirror
> ​
> Now apply the patch to your cluster.

```bash
$ curl \
    --header "Content-Type: application/json" \
    --header "Authorization: Bearer $JWT_TOKEN" \
    --request PATCH \
    --data  @$install_config_patch \
"https://api.openshift.com/api/assisted-install/v2/clusters/$CLUSTER_UUID/install-config"
```

> **NOTE:** You may find that the above command fails. The JWT_TOKEN expires very quickly. If you get a 405 error, re-run the command that creates your JWT_TOKEN from the [User Authentication](#user-authentication) above.

### Validate your install-config

```
curl -s -X GET \
  --header "Content-Type: application/json" \
  -H "Authorization: Bearer $JWT_TOKEN" \
  "https://api.openshift.com/api/assisted-install/v2/clusters/$CLUSTER_UUID/install-config" \
  | jq -r
```

Ensure that the resulting output has a "imageContentSource", "additionalTrustBundle" and a "pullSecret" that contains your internal registry.

## Complete your cluster build

You can now follow published procedures to complete your cluster install from the Assisted Installer.

## Enabling the Mirrored Operator Catalog Post Steps

As a reminder, you will need to apply an updated ImageContentSourcePolicy object to your cluster in order to leverage the mirrored Operator catalog as documented in the [Post Install Mirrored Catalogs](https://docs.okd.io/latest/post_installation_configuration/preparing-for-users.html#post-install-mirrored-catalogs) document.
