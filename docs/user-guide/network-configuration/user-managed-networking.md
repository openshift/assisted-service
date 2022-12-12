# User Managed Networking

User managed networking is a feature in Assisted Installer that allows customers with non-standard network topologies to deploy OCP clusters. Examples are

- customers with own loadbalancer who do not wish to use keepalived and VRRP for handling VIPs
- deployments with cluster nodes distributed across multiple distinct L2 network segments

## Default configurations

| Platform  | User Managed Networking |
| --------- | ----------------------- |
| Baremetal | False                   |
| None      | True                    |
| vSphere   | False                   |

## Validations

There are various network validations happening in Assisted Installer before it allows for the installation to start. When User Managed Networking flag is enabled, the following validations change

- L3 connectivity check (ICMP) is performed instead of L2 check (ARP)
