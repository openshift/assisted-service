---
title: Simplify manifest directory structure
authors:
  - "@pmaidmen"
creation-date: 2024-06-09
last-updated: 2024-06-09
---

# Simplify manifest directory structure

# Background:

It is important to start with a description of how the current functionality works;

Custom manifests are stored in an "object store", this may be within an object store container or within the Amazon S3 service, depending on where and how the assisted service is deployed.

Manifest storage is divided into two directories

/manifests and /openshift

Manifests may be uploaded to either directory via the assisted service.

It is unclear why we need to use two directories to store these manifests as ultimately they are merged into a single directory prior during the creation of the bootstrap node. (See https://issues.redhat.com/browse/MGMT-15356)This could be considered to be quite confusing to end users.

Manifests are configuration files that are applied by openshift during the installation phase. These are in .yaml, yml or json format. It is also possible to upload "patch" files that will alter yaml or yml files within the openshift or manifest directories.

# Summary

We would like to improve the custom manifest feature to make it more consistent, more reliable and more user friendly. By addressing the following points

* The use of two manifest directories `openshift` and `manifests` appears to be uneccessary, we should siplify this.

# Motivation

Reflecting on the current structure of the manifest upload feature, the presence of two manifest directories is both a source of extra maintenance and a source of confusion.

It would make for a simpler user experience to simply provide a manifest upload where the user does not have to specify any directory.

### Goals

- To simplify the storage of manifest metadata.

### Non-Goals

- No "non goals" have been identified at this point.

## Proposal: Remove the requirement to use a folder name for manifest upload

* All manifests will be uploaded to `manifests` in future, no manifests will be creted in `openshift`
* Existing manifests in `openshift` will need to be moved to `manifests` in a migration

### User Stories

#### Story 1 - (API user) Happy path new file creation

* An API user uploads a valid manifest file using the manifest API
* All validations pass and the upload proceeds
* The user should receive an acknowledgement of the file they uploaded from the API.

#### Story 4 - (API user) Upload failure

* An API user uploads a valid manifest file using the manifest API
* All validations pass and the upload proceeds
* There is a problem during update
* The user should receive an error telling them the upload failed.


### Implementation Details/Notes/Constraints [optional]

There are approx 3500 live clusters in SAAS, if we allow a conservative estimate of 10 manifests per cluster, this will involve 4 writes for every file, once to create the new file, once to delete the old file, two additional calls to handle "user-generated" vs "system-generated"

### Risks and Mitigations

There should be a migration of historical data in order for this to work.
It may be wise to await the enhancement in MGMT-18163 as this would greatly simplify the work, reduce the number of writes required and would save a lot of S3 charges.

## Design Details [optional]

### Open Questions

  - What is the data retention policy for cluster data, how long do we have to keep this data immediately accessible for a user?
  - What occurs for archival and would this be relevant/non relevant to a data migration (i.e. do we ever need to "defrost" an archive)
  - How many active clusters do we have in assisted?
  - How much file data do we have in S3, how many files on average per customer?
  - Do we have examples of previous migrations, how they were handled, repositories and files involved?
  - What approach is taken to testing of migrations prior to applying to the database, is staging a good stand in for production for this purpose?

### UI Impact

This will have a significant UI impact as the UI refers to the directory structures.

### API Impact

This change would require an API change and as such is non trivial.
We would have to consider how existing clients use the UI as this would potentially present a breaking change.

### Test Plan

Unit tests and where necessary subsystem-tests will be written to assert correct behavior

#### Manual tests
* A manual test of any migration script will need to be performed in a staging environment
* A manual test of the migration script against an assisted-test-infra cluster would help to evaluate the value of this for customers

## Drawbacks

* This change would require an API change and as such is non trivial.
* We would have to consider how existing clients use the UI as this would potentially present a breaking change.
* This will have a significant UI impact as the UI refers to the directory structures.

## Alternatives

- We could leave the implementation as it stands today
- We could make this change in smaller steps, perhaps by making the "folder" API optional, existing API users could continue to use this as they wish, we would focus on removing the folder option from the UI.
