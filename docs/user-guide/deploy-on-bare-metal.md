# Openshift deployment with OAS - On Bare Metal
This guide contains all the sections regarding Bare Metal deployment method, like iPXE/PXE, VirtualMedia, etc... let's get started

## General

This section is generic for the most of the cases:

- DHCP/DNS running on the network you wanna deploy the OCP cluster.
- Assisted Installer up & running (It's ok if you're working with cloud version).
- Typical DNS entries for API VIP and Ingress VIP.
- Pull Secret to reach the OCP Container Images.
- SSH Key pair.

_*Note*: This method could be used also in Virtual environment_

- With that we could start, first step is create the cluster
- Fill the Cluster name and Pull Secret fields, also select the version you wanna deploy:

![img](img/new_cluster.png)

- Now fill the Base Domain field and the SSH Host Public Key

![img](img/entry_base_domain.png)
![img](img/entry_ssh_pub_key.png)

- Click on _Download Discovery ISO_

![img](img/entry_ssh_pub_key.png)

- Fill again the SSH public key and click on _Generate Discovery ISO_

![img](img/entry_ssh_download_discovery.png)

- Wait for ISO generation to finish and you will reach this checkpoint

![img](img/discovery_iso_generated.png)


## iPXE

iPXE deployment method

*NOTE1*: We use a sample URL, please change to fit your use case accordingly
*NOTE2*: We've set the live_url as the node hostname on 8080 port , please change to fit your use case accordingly

### Automatic

The automatic way is done using podman, just follow this steps:

```shell
IPXE_DIR=/tmp/ipxe/ai
mkdir -p ${IPXE_DIR}

# This command will download the ISO, extract the Images and create the ignition config files
podman run -e BASE_URL=http://devscripts2ipv6.e2e.bos.redhat.com:8080 -e ISO_URL=$(curl http://devscripts2ipv6.e2e.bos.redhat.com:6008/api/assisted-install/v2/infra-envs/29b516fd-8f3a-42bf-8a59-cb20ef2630e0/downloads/image-url | jq ".url") -v /tmp/ipxe/ai:/data:Z --net=host -it --rm quay.io/ohadlevy/ai-ipxe

# This command will host the iPXE files on an podman container
podman run  -v ${IPXE_DIR}:/app:ro -p 8080:8080 -d --rm docker.io/bitnami/nginx:latest
```

To ensure if your container is working fine, check the url with a `curl` command

```shell
curl http://$(hostname):8080/ipxe
```

### Manual

The manual way is explained here. You need at least to have the Discovery ISO already generated

Now let's download that ISO in the provisioning machine, where the iPXE files will be hosted (use the _Command to download the ISO_ button from the Assisted Service website

```shell
IPXE_DIR=/tmp/ipxe/ai
IMAGE_PATH=/tmp/discovery_image_ocp.iso
ISO_URL=$(curl http://console.redhat.com/api/assisted-install/v2/infra-envs/<infra_env_id>/downloads/image-url | jq ".url")

wget -O ${IMAGE_PATH} ${ISO_URL}
```

- Now we need to create the folder and the _ipxe_ file definition

```shell
mkdir -p ${IPXE_DIR}

cat > $IPXE_DIR/ipxe << EOF
#!ipxe
set live_url $(hostname):8080
initrd --name initrd \${live_url}/initrd.img
kernel \${live_url}/vmlinuz initrd=initrd ignition.config.url=\${live_url}/config.ign coreos.live.rootfs_url=\${live_url}/rootfs.img ${KERNEL_OPTS}
boot
EOF
```

- We also need to extract the images from the ISO

```shell
PXE_IMAGES=$(isoinfo -R -i $IMAGE_PATH -f | grep -i images/pxeboot)

for img in $PXE_IMAGES; do
  name=$(basename ${img})
  echo extracting $name
  isoinfo -R -i $IMAGE_PATH -x $img > $IPXE_DIR/$name
done
```

- And as a last step, write the Ignition files for the deployment

```shell
echo writing custom user ignition
ziptool=$(isoinfo -R -i $IMAGE_PATH -x /images/ignition.img | file -b - | cut -d ' ' -f 1)
isoinfo -R -i $IMAGE_PATH -x /images/ignition.img | ${ziptool,,} -dc | cpio -iD $IPXE_DIR
```

- After the Ignition files creation we need to host the files, for that we will use a podman contianer based on nginx

```shell
podman run  -v ${IPXE_DIR}:/app:ro -p 8080:8080 -d --rm docker.io/bitnami/nginx:latest
```

- To ensure if your container is working fine, check the url with a `curl` command

```shell
curl http://$(hostname):8080/ipxe
```

### Booting the nodes from iPXE

- First step, we need to set up the boot mode on the iDrac's as `boot once` for iPXE, this will depend on the steps on every Bare Metal Manufacturer/Version/Hardware.
- When you are booting the nodes, stay tuned to press `crtl-b` when the prompt say that:

![img](img/iPXE_boot.png)

- Now we need to get a correct IP and point to the right iPXE file
- And we just need to wait until the boot was finished, and the nodes start appearing on the Assisted Service interface

![img](img/manual_ipxe_boot.png)

![img](img/boot_from_ipxe.gif)

- Then we will modify the nodename to use a right name for Openshift

![img](img/ai_node_appear.gif)

- Create another 2 more nodes and repeat this step

![img](img/ai_all_nodes.png)

- Now fill the _API Virtual IP_ and _Ingress Virtual IP_ fields

![img](img/ai_vips.png)

- Now you just need to click on _Install Cluster_ button and wait for the installation to finish.


