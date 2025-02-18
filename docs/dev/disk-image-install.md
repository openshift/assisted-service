# Assisted Installation With a Disk Image

You can use the Assisted Installer to install directly to the existing root filesystem of a disk image.
This should be done for platforms that cannot take the discovery ISO as boot media, and also cannot use iPXE.

## Available Disk Images

You can download disk images for use with this process at https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/
An example for installing OCP 4.17 with libvirt would be https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.17/4.17.2/rhcos-4.17.2-x86_64-qemu.x86_64.qcow2.gz

## Discovery and Installation

You must write the Assisted Installer discovery ignition to the disk on first boot in order to install in this way.
You can do this using whatever metadata service is available for your [target platform](https://coreos.github.io/ignition/supported-platforms/#supported-platforms).

You can download the discovery ignition using the assisted-service REST API.
A presigned URL is published on the InfraEnv status (`.status.bootArtifacts.discoveryIgnitionURL`) if using the kubernetes APIs.
Otherwise you can download it using the endpoint `/v2/infra-envs/{infra_env_id}/downloads/files` with the query parameter `file_name=discovery.ign`

Once the Assisted Installer agent is running, discovery will proceed as normal outside of some disabled validations (specifically the disk speed check which writes directly to the target install device).
When the host is installed the OS content from the selected release image will be installed to a new [ostree stateroot](https://ostreedev.github.io/ostree/deployment/) and the install ignition will be applied to it when it boots for the first time.
During this first boot the initial stateroot and boot files are also cleaned up by the `cleanup-assisted-discovery-stateroot` service.
