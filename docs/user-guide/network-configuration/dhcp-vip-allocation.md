# DHCP VIP Allocation

The VIP DHCP Allocation is a feature allowing users to skip the requirement of manually providing virtual IPs for API and Ingress by leveraging the ability of a service to automatically allocate those from the DHCP server present in the network.

If the feature is enabled, instead of using `api_vip` and `ingress_vip` from the cluster configuration, the service will send a lease allocation request and based on the reply it will use VIPs accordingly. The IPs will be allocated from the subnet defined as Machine Network.

Please note this is not an OpenShift feature and it's been implemented in the Assisted Service in order to facilitate the configuration.

## Sample REST API payload

### Enabling autoallocation

```json
{
  "vip_dhcp_allocation": true,
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "cluster_networks": [{ "cidr": "10.128.0.0/14", "host_prefix": 23 }],
  "service_networks": [{ "cidr": "172.30.0.0/16" }],
  "machine_networks": [{ "cidr": "192.168.127.0/24" }]
}
```

### Disabling autoallocation

```json
{
  "api_vip": "192.168.127.201",
  "ingress_vip": "192.168.127.202",
  "vip_dhcp_allocation": false,
  "network_type": "OVNKubernetes",
  "user_managed_networking": false,
  "cluster_networks": [{ "cidr": "10.128.0.0/14", "host_prefix": 23 }],
  "service_networks": [{ "cidr": "172.30.0.0/16" }]
}
```

## Additional reading

[Bare metal IPI documentation](https://docs.openshift.com/container-platform/4.10/installing/installing_bare_metal_ipi/ipi-install-installation-workflow.html#additional-install-config-parameters_ipi-install-installation-workflow) provides additional explanation of the syntax for the VIP addresses.
