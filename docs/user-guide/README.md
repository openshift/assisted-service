# Assisted Installer

Assisted Installer is available at https://console.redhat.com/openshift/assisted-installer

# User Guide

Welcome to the Openshift Assisted Service User Guide.

Here we will look for the best way to help you deploying Openshift 4 in the provider you desire.

 - [OCP Deployment on Local](deploy-on-local.md)
 - [OCP Deployment on Bare Metal](deploy-on-bare-metal.md)
 - [OCP Deployment on vSphere](deploy-on-vsphere.md)
 - [OCP Deployment on RHEV](deploy-on-RHEV.md)
 - [OCP Deployment on Openstack](deploy-on-OSP.md)

### Using the RESTFul API

The assisted-service exposes a RESTFul API which is described in [swagger.yaml](../../swagger.yaml).

A guide of using the RESTFul API is available on [rest-api-getting-started.yaml](./rest-api-getting-started.md).

### Using Assisted Service On-Premises

Please refer to the [Hive Integration readme](../hive-integration/README.md) to learn how to install OCP cluster using Assisted Service on-premises with [Hive](https://github.com/openshift/hive/) and [RHACM](https://github.com/open-cluster-management) (Red Hat Advanced Cluster Management).

### Network Configuration

Please refer to the [Network Configuration introduction](network-configuration/README.md) for more information about advanced network configuration with the Assisted Service.

### Infrastructure Operator

Please refer to [Infrastructure Operator installation](infrastructure-operator-olm.md) for more information on installing the Hive integration flavour of Assisted Installer via OLM.

### Using Assisted Installer hosted in console.redhat.com with local image registry

Please refer to [Saas + on premise registry](cloud-with-mirror.md) for more information on installing an OCP cluster leveraging the [console.redhat.com](https://console.redhat.com) Assisted Installer with a local mirror registry.
