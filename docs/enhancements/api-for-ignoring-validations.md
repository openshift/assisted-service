---
title: api-for-ignoring-validations
authors:
  - "@pmaidmen"
creation-date: 2023-01-08
last-updated: 2023-02-22
---

# Allow validations to be ignored

# Summary

We need a new api that will allow us to ignore cluster/host validations, this will be for unsupported use.
This API should work on a per cluster basis and will have its own organisation level permissions to allow us to enable it for specific organisations.
An event should be emitted whenever an installation is started and this feature has been used. Additionally a feature flag should be raised to indicate this usage also.
Clusters containing ignored validations should not contribute to statistics as they are unsupported.
Clusters containing ignored validations should not be added to triage as they are unsupported.
# Motivation

Some users have very specific HW that doesn't pass our validations and we want to allow them to proceed with installation.
Sometimes we have bugs in our validations that block people from installing and we don't want our partners left in a position where they can't install.

### Goals

- Allow validations to be ignored so that installation may proceed.

### Non-Goals

- This is not intended for broad customer use and is not a "go to" solution for every validation problem
- This should not add anything to the cluster API, this is unsupported and should not show up when a cluster is queried
  - Instead there will be a specific API

### The API
This will consist of two endpoints.

GET /v2/clusters/{cluster_id}/ignored-validations

Will return the ignored host and cluster validations as a pair of JSON string arrays

```
{
  "ignored_host_validations": "[\"has-memory-for-role\", \"has-cpu-cores-for-role\"]"
  "ignored_cluster_validations": "[\"network-type-valid\", \"ingress-vips-valid\", \"ingress-vips-defined\"]"
}
```

PUT /v2/clusters/{cluster_id}/ignored-validations

Will accept the ignored host and cluster validations as a pair of JSON string arrays

```
{
  "ignored_host_validations": "[\"has-memory-for-role\", \"has-cpu-cores-for-role\"]"
  "ignored_cluster_validations": "[\"network-type-valid\", \"ingress-vips-valid\", \"ingress-vips-defined\"]"
}
```

It is possible for to set in this way

```
{
  "ignored_host_validations": "[\"all\"]"
  "ignored_cluster_validations": "[\"all\"]"
}
```

The effect of which is to ensure that all ignorable validations are ignored.
When ignoring validations, there will be some "absolutely mandatory" validations that cannot fail prior to proceeding. If the user attempts to explicitly ignore these, the endpoint should fail with an error.

For cluster level validations, these are:
```
"api-vips-defined", "ingress-vips-defined", "all-hosts-are-ready-to-install", "sufficient-masters-count", "pull-secret-set", "cluster-preparation-succeeded"
```

For host level validations, these are: 
```
"connected", "has-inventory", "machine-cidr-defined", "hostname-unique", "hostname-valid"
```

### The implementation of ignored-validations
    * All validations run.
    * One or more validations fail and describe a problem.
    * The user evaluates the alleged problems and decides to try the installation anyway.
    * If the user ignores validations (by making a change to a boolean in the cluster object), we should
        * Emit an event
        * Record the validation ignore in the cluster in some way
        * Log the validations that were failing at the moment the validations were ignored.

### Effect on Triage tickets.
To ensure that there is less noise for Triage tickets, any cluster that has events consistent with this sort of override should be omitted from Triage ticketing.

Example here: https://github.com/openshift-assisted/assisted-installer-deployment/blob/463dc6612c923c46d47e80762c72651758a690d3/tools/create_triage_tickets.py#L64

### Organisation based feature enablement.
This feature is to be enabled on a per organisation basis,
so will need to use the "per customer feature enablement" feature.

See the following link for more information.

https://github.com/openshift/assisted-service/blob/master/docs/dev/feature-per-customer.md

### User Stories

#### Story 1

As a user with known validation issues that need to be ignored, I would like to be able to ignore VIP validation in cases where VIPS could be incorrectly shown as busy. So that I may proceed with installation.

### Risks and Mitigations

- We should not add uneccesary noise to triage tickets with this, it is therefore quite important to add some signatures to indicate that these validations have been ignored or to handle in a way that ensures they never appear as Triage tickets.

### Open Questions

N/A

### UI Impact
There will be no UI impact of ignoring a validation as all non mandatory validations will be ignored and there will be no feature allowing manipulation of this via UI.
### Test Plan

A subsystem test that runs a set of validations (with some failures) and then sets the ignore setting for the cluster. Witnessing that installation is allowed to proceed.
Some manual tests to verify good behaviour.
Unit tests as required.

## Drawbacks

* We will have to be careful when examining bug reports and triage tickets to ensure we are aware of when this feature has been used.
* Might allow some customers to ignore things they shouldn't ignore.
