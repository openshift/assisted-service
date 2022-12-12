# Openshift Deployment on OpenStack by OpenShift Assisted Service

This guide explains how to deploy OpenShift by the OpenShift Assisted Service
on OpenStack.

---

**NOTE**

Currently, the deployment of an OpenShift Cluster on
Red Hat OpenStack Platform is blocked by OpenShift Assisted Service, while RDO is working fine.
The related check can be disabled by including `valid-platform` the environment variable
`DISABLED_HOST_VALIDATIONS` in the context of the OpenShift Assisted Service, e.g. like this:

```
DISABLED_HOST_VALIDATIONS=valid-platform,container-images-available
```

---

## Requirements

- Two floating IP addresses like [other ways of installing OpenShift](https://github.com/openshift/installer/tree/master/docs/user/openstack#create-api-and-ingress-dns-records), it is recommended that DNS resolves
  - api.CLUSTERNAME.DOMAIN to the first floating IP address, and
  - \*.apps.CLUSTERNAME.DOMAIN to the second one
- The resources required by the VMs
- A dedicated OpenStack network and subnet are recommended

## Steps

1. Generate the discovery iso in OpenShift Assisted Service.
2. Upload the discovery iso as a new image to OpenStack.
3. Create two ports and associate each one a floating IP address.
4. Create the VMs to run the OpenShift cluster, with a bootable fresh volume
   and the image of the discovery iso as the second boot index. Please find an example below.
5. Add the addresses of both floating IPs as the `allowed_address_pairs` of all VMs which
   might run a master role in the OpenShift cluster. This will enable the virtual IPs to be
   usable on the VMs.
   Upon adding an IP address to the "allowed_address_pairs" field in the Neutron's port
   the ML2/OVN driver will check if that IP matches with the IP of another existing port
   in the same network (Logical_Switch in OVN) and, if they do match, ML2/OVN will update
   the type of the matching port to "virtual".
   Please the details in
   [Deploying highly available instances with keepalived](https://docs.catalystcloud.nz/tutorials/compute/deploying-highly-available-instances-with-keepalived.html#deploying-highly-available-instances-with-keepalived) and
   [Highly available VIPs on OpenStack VMs with VRRP](https://blog.codecentric.de/en/2016/11/highly-available-vips-openstack-vms-vrrp/)
   .
6. Assign an appropriate security group to the networking ports of the VMs
   and to the ports of the floating IPs. A security group that allows all IP traffic works.
7. Install the OpenShift cluster via OpenShift Assisted Service as it would be on bare metal.

## Example Block Device Mapping

```
"block_device_mapping_v2": [
    {
        "uuid": ID_OF_FRESH_VOLUME,
        "boot_index": "0",
        "source_type": "volume",
        "destination_type": "volume",
        "delete_on_termination": True
    },
    {
        "uuid": ID_OF_DISCOVERY_ISO,
        "source_type": "image",
        "volume_size": "1",
        "device_type": "cdrom",
        "boot_index": "1",
        "destination_type": "volume",
        "delete_on_termination": True
    }
]
```
