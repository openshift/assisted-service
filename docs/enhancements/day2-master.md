---
title: day2-master
authors:
  - "@oamizur"
creation-date: 2022-07-10
last-updated: 2022-07-24
---

#Install day2 master
##Summary
Sometimes there is a need to install a day2 master to an existing cluster. When using assisted installer, this flow is not supported, since only worker installation is supported for day2 flow.
##Motivation
There are several reasons there would be a need to install day master including:

- An existing master node is malfunctioning

- An existing master node does not meet new hardware requirements
  ##Goals
  Support adding a master node in SaaS and ACM

##Non-goals
Remove or clean old node

Manage etcd: Etcd management is out of scope and will be handled for now by the user interacting with etcd-operator.

##Proposal
Enable assisted-service to add master nodes. With the API, the role can be set to “master”, and the host will boot using the cluster’s master ignition.

Node approval will be the same as today’s process for approving worker nodes.

##User Stories
As a user of ACM and/or the SaaS, I can set an agent’s role to “master” and assign it to a cluster, and it will be added to that cluster as a new master node.

##Open Questions
Vsphere: should this process be handled by machine-api? Or is there any role for assisted-service to handle master replacement when using vsphere?
