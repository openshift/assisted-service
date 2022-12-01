---
title: Add a MetalLB Operator to Assisted Installer
authors:
  - "@flaper87"
creation-date: 2022-12-15
last-updated: 2022-12-15
---

# Add MetalLB operator

## Summary

This enhancement focuses on the creation of relocatable clusters. A relocatable cluster is a cluster
that can be moved from one location to another without cluster reconfigurations and without
depending on a specific network topology. To this end, this enhancement proposaes adding support for
the MetalLB operator to assisted installer.


## Motivation

The number of requests to create relocatable OCP clusters seem to be increasing and the idea of
creating OCP appliances is becoming a common goal. Today there are solutions like ZTPFW that solve
this problem by following a set of tasks that allow for these clusters to be relocated and for OCP
to act as an appliance.

Some of this tasks could be made part of components that are closer to the core so that they can be
leveraged in other environments like SaaS. This enhancement proposes moving one of these tasks to
Assisted Installer. Specifically, this enhancement proposes to deploy the MetalLB operator using
Assisted Installer so that it can create clusters that can be relocated.

### Goals

- To add a MetalLB operator to Assiste Installer
- To add the required configurations for this operator to be able to not just install the cluser but
  also have it properly configured and ready for relocation.


### Non-Goals

- Change the way network configuration is managed today


## Premises

An Edgecluster must be connected to 2 networks. We will refer to these networks as Insternal network
and External network.

The **Internal network** is the network used to deploy the cluster on. In other words, this is the
network you will see in the *INTERNAL-IP* of a node when running `oc get nodes -owide`. This network
should be considered **immutable**. Once it is set, it should not be changed.

The **External network** is the mutable network. It is a network that can be changed during the
relocation.

In this scenario, MetalLB acts as a secondary load balancer for the OCP cluster. Specifically, it is
meant to be used as a load balancer for traffic coming from the external network and forward this
traffic to the internal services of the cluster. Here's an example of what the IPAddressPool
(MetalLB CR) and the Service definition look like in the case of the apiserver:

```yaml
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: metallb-api-vip
  namespace: metallb
spec:
  protocol: layer2
  autoAssign: false
  addresses:
    # We will be discussing this further down in the enhancement
    - $METALLB_IP/32
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    # This is used by MetalLB to know from which address
    # pool it will take an IP for this service
    metallb.universe.tf/address-pool: metallb-api-vip
  name: metallb-api
  namespace: openshift-kube-apiserver
spec:
  ports:
  - name: http
    protocol: TCP
    port: 6443
    targetPort: 6443
  selector:
    app: openshift-kube-apiserver
  type: LoadBalancer
```

## Proposal

Adding an operator to Assisted Installer is a fairly simple task. This operator, however, requires a
bit more input for Assisted Installer to be able to configure it properly. Specifcally, Assisted
Installer needs to be able to answer the following questions:

- Which is the *internal network*?
- What is the IP on the **external network** to use for the API VIP?
- What is the IP on the **external network** to use for the Ingress?
- What routes (if any) should be used for the **external** API and Ingress endpoints?

### Identifying Internal Network

Assisted installer already has mechanims to know which is the internal network. Internally this
network is called the `MachineNetwork`. In case of SNO, the `MachineNetwork` is required when defining
the ACI. In the case of a multinode deployment, it is inferred from the API VIP whereas in the case
of dual stack this information is required the same way as in the SNO case.

Regardless, no changes should be required in assisted installer to identify the internal network.

### Identifying External Network

This enhancement proposes requesting the user for the following information in order to prepare the
cluster for relocation:

- External API VIP: This is an IP on the **external** network
- External Ingress IP: This is an IP on the **external** network
- External base domain: A domain to use for the **external** traffic (optional).

Unlike for the **internal** network, this enhancement proposes to always infer the subnet of the
external network from the External API VIP and the External Ingress IP provided by the user. There
are utilities in Assisted Installer to do this already.

Some requirements:

- The **external** API VIP and Ingress IP must be on the same subnet.
- The host must be connected to this network and the network must be routable
- These 2 IPs must be optional. The IPAddressPool and Service won't be configured if this information is not provided
- Either both are IPs are provided or none.
- The *internal* network must be configured as a static network to prevent the MachineNetwork to
change and, therefore, the IP of each of the nodes.

### User Stories

#### Story 1

As a hardware provider, I want to be able to deliver an OCP appliance that can be shipped to a
different location and reconnected.

#### Story 2

As a datacenter administrator, I want to be able to install an OCP cluster whose IP can be
reconfigured as well as its domain.

### Implementation Details/Notes/Constraints [optional]

1. It's currently not possible to enable operators when using KubeAPI. Implementing this feature,
without enabling it through KubeAPI will limit the scope of it for environments that use ZTP. It
should be considered to allow enabling operators through KubeAPI either as part of this enhancement
or a separate one.
2. Assisted Installer doesn't have a way to configure an operator. Operators can be enabled or
disabled. The rest of the information they take from the infrastructure data that has been
collected. For this enhancement, we need to either introduce the concept of External API VIP and
External Ingress IP to the cluster or allow for specifying operator-specific configurations.

### Risks and Mitigations

Previous sections of the enhancement state that the MachineNetwork must be static for this workflow
to work as expected. It is important that this network won't change during a relocation or at any
point during the lifecycle of the cluster.

This requirement raises the risk of this enhancement as it introduces new infrastructure
requirements during the deployment and at the new location in case of relocation. Specifically, in
the case of a customer using a network accessible to more than one cluster as their MachineNetwork,
there's a risk of having IP clashes.

People using ZTPFW have solved this issue by providing an isolated, routable, network as the
MachineNetwork. This is done either by using a top of the rack switch or some other mechanism. This
is particularly relevant for multinode deployments where hosts need to be able to talk to each
other.

Finally, the UX won't be as straightforward when building a relocatable cluster as users will have
to be familiar with the 2 networks that are needed.

## Design Details [optional]

### Open Questions

### UI Impact

It will be required to adapt the UI to interact with this feature. No breaking changes will be
introduced by this enhancement.

### Test Plan


## Drawbacks


## Alternatives

- To have Assisted Installer deploy the operator and then leave the rest of the configurations to
the users. This has the benefit of simplifying the deployment workflow but it will worsen the user
experience for day-2 operations. Skipping the pre-configuration of the cluster's network (internal
and external networks) may result in broken clusters should the IP for the internal network change.


- To not present this functionality as a cluster profile rather than as yet-another-operator that
can be installed. The user would choose to deploy an "edgecluster" rather than enabling MetalLB.
This would ease the introduction of the new requirements, it would make it clear that the cluster
being installed is not a regular one, and it will scope the use case.

