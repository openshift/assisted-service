# Platform Selection

The Assisted Installer currently supports the following OpenShift platforms:
- BareMetal
- VSphere
- Nutanix
- None

Select the platform in the AgentClusterInstall CR, via `spec.platformType`.

In some cases, the system will automatically set the platform type:
- The default platform for multi-node clusters is BareMetal
- The default platform for Single Node OpenShift is None
- Enabling `spec.networking.userManagedNetworking` without specifying the platform will cause the platform to be None. BareMetal platform and userManagedNetworking are not compatible.

The platform that will be used can also be seen in the AgentClusterInstall CR, via
`status.platformType`.
