---
title: Transactional Custom Manifests
authors:
  - "@pmaidmen"
creation-date: 2024-06-09
last-updated: 2024-06-09
---

# Transactional Custom Manifests

# Background:

It is important to start with a description of how the current functionality works;

Custom manifests are stored in an "object store", this may be within an object store container or within the Amazon S3 service, depending on where and how the assisted service is deployed.

Manifest storage is divided into two directories

/manifests and /openshift

Manifests may be uploaded to either directory via the assisted service.


Manifests are configuration files that are applied by openshift during the installation phase. These are in .yaml, yml or json format. It is also possible to upload "patch" files that will alter yaml or yml files within the openshift or manifest directories.

Manifest and patch files may be marked as either "system generated" or "user generated", by default a file is considered "system generated". 

To mark a file as user generated, a metadata file is created under the subtree `manifest-attributes` to match the file and path in the subtree `manifests`

So if we have a file:
`manifests/openshift/user-defined-manifest.yaml`

The metadata file to mark this as user-supplied would be:
`manifest-attributes/openshift/user-defined-manifest.yaml/user-supplied`

As we are using the filesystem as a means of storing metadata about a file, this needs to be performed in two operations, one to write the file and one to write the metadata.

This has a weakness; by performing two writes, we risk that the second write may not take place in the case of an S3 failure. 

# Summary

We would like to improve the custom manifest feature to make it more consistent, more reliable and more user friendly. By addressing the following points

* Find a more reliable and "transactional" way in which the upload of manifest files may be handled.

# Motivation

We have had a recent bug report about an edge case

  The following bug was contributed to by this issue: https://issues.redhat.com/browse/MGMT-17802

In this edge case, if there is a failure in S3 with unfortunate timing then it is possible to store a manifest with inconsistent metadata.
This is not a situation that is acceptable moving forward and we need to do something.

There are some problems with the existing implementation of manifest storage when it comes to metadata

* A manifest upload cannot be "transactional" under the current design, raising the risk that one part of the upload may fail and leave the rest inconsistent.

### Goals

- To ensure that manifests may be written atomically so that their reliability may be guaranteed.
- To simplify the storage of manifest metadata.

### Non-Goals

- It is a non-goal to change the behavior of the UI for manifests.

## Proposal: Use the Amazon S3 metadata API 

We should change the way that we store metadata about a manifest, instead of using an awkward "directory based" approach to metadata, we should instead use the built in facilities of S3 which has native Metadata support for objects.
This should be achievable in a single write and should mitigate the transactionality issues.

### Object store API changes required:

	#### Uploads

	S3Wrapper API:
	Calls to Upload, UploadFile and UploadStream will need to be updated to support metadata for an object
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L42-L43
	
	##### Filesystem implementation:
	
	Implementation changes to the filesystem S3Wrapper client will be required in order to implement metadata support.
	We should attempt to keep this atomic by using XATTRs if supported.
	
	Implementation of Upload in S3Wrapper API (filesystem):
	Need to be updated to support metadata for an object (possibly based on XATTRs
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/filesystem.go#L55-L70
	
	Implementation of UploadFile in S3Wrapper API (filesystem):
	Need to be updated to support metadata for an object (possibly based on XATTRs
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/filesystem.go#L72-L84
	
	Implementation of UploadStream in S3Wrapper API (filesystem):
	Need to be updated to support metadata for an object (possibly based on XATTRs
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/filesystem.go#L86-L136
	
	##### S3 implementation:
	
	We will need to ensure that metadata is passed during upload, in theory this should be quite simple;
	
	Implementation of Upload in S3Wrapper API (S3):
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L199-L202
	
	Implementation of UploadFile in S3Wrapper API (S3):
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L195-L197
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L180-L193
	
	Implementation of UploadStream in S3Wrapper API (S3):
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L176-L178
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L151-L174 <- UploadInput.Metadata is the field we need to pass
	
	#### Downloads
	
	S3Wrapper API:
	Calls to Download will need to be updated to handle metadata, presently we only parse the Body of the response from S3. 
	The Metadata field is available for use, but we are not using it. As I see it, these return values will need to change `(io.ReadCloser, int64, error)`
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/client.go#L45
	
	Implementation of Download in S3Wrapper API (filesystem):
	Needs to be updated to return the metadata for an object. Needs to fetch this from the filesystem (possibly XATTR)
	https://github.com/openshift/assisted-service/blob/master/pkg/s3wrapper/filesystem.go#L138-L158
	
### Consumers of the object store API will need to change, to consume the metdata and use it

	Calls to `objectHandler.Download` and `objectHandler.UploadFile` and `objectHandler.UploadStream` will need to change to accomodate the metadata,
	the metadata itself may be ignored in many cases if not required.

	For manifests, we will need to use metadata to implement.
	
		"generated-by" => "system-generated"
		
		or 
		
		"generated-by" => "user-generated"
	
	Functions where "generated-by" will need to be processed
	
	https://github.com/openshift/assisted-service/blob/master/internal/manifests/manifests.go#L55
	
		* Fix the implementation of uploadManifest to pass the metadata field for "generated-by" 
		* Remove the "markUserSuppliedManifest" implementation
		
	https://github.com/openshift/assisted-service/blob/master/internal/manifests/manifests.go#L151
		* Change `ListClusterManifestsInternal` to fetch objects with their custom metadata
		* Use custom metadata field "generated-by" to determine what items we return from list
			- Remove approach that uses `IsUserManifest` but don't delete the `IsUserManifest` function as this will be handy for migrations.
			
	https://github.com/openshift/assisted-service/blob/master/internal/manifests/manifests.go#L213	
		* Fix the implementation of uploadManifest to pass the metadata field for "generated-by" 
		* Remove the "markUserSuppliedManifest" implementation	
		
	Generally, remove all calls to `markUserSuppliedManifest` and `unmarkUserSuppliedManifest` as we will no longer be taking this approach,
	keep `markUserSuppliedManifest` and `unmarkUserSuppliedManifest` functions for use in migrations.

	In summary, manifest creation, update and list operations are affected by this change.
### User Stories

#### Story 1 - (API user) Happy path new file creation

* An API user uploads a valid manifest file to openshift/manifests using the manifest API
* All validations pass and the upload proceeds
* The user should receive an acknowledgement of the file they uploaded from the API.
* If the user attempts to list the cluster manifests, they should see the uploaded file as it's a user supplied manifest.

#### Story 2 - (API user) List manifests - default mode

* The user takes no specific action with manifests
* The user starts installation
* Once installation has started, theyb attempt to list cluster manifests, none should be shows as there are no user manifests.

#### Story 3 - (API user) List manifests - passing include-user-generated parameter

* The user takes no specific action with manifests
* The user starts installation
* Once installation has started, theyb attempt to list cluster manifests, the system generated manifests should be listed.

#### Applying the migration (Moving from an older version of assisted to one that supports the new storage methodology):

	Migration to this feature:
		For each manifest in each cluster;
			determine whether manifest is `user-supplied` or `system-generated` using the file path method. (IsUserManifest)
				upload a new revision of the manifest with the appropriated "generated-by" value in the metadata
				delete the file path marker if applicable (`unmarkUserSuppliedManifest`)
	
	Rollback of feature migration:
		For each manifest in each cluster;
			determine whether manifest is `user-supplied` or `system-generated` using the metadata method ("generated-by")
				create a file path marker if the file is `user-supplied` (`markUserSuppliedManifest`)
				metadata may be left in place as it will not be used by the old implementation anyway.
			
#### Rolling back the migration: (Moving from a version of the service that supports the new storage methodoly to an older version)

	For each cluster
		For each manifest in cluster (as determined by database + storage)
			Ensure that appropriate manifest S3 metadata record exists for any files that are "user-generated"
			Remove the database record for the files that have been processsed

There are approx 3500 live clusters in SAAS, if we allow a conservative estimate of 10 manifests per cluster, this will involve no S3 writes for a "roll forward" and up to 1 write per file for a "roll forward"

### Risks and Mitigations

We must deal with the migration of historical data in order for this to work.
We need to make sure this works for SAAS and for ACM/MCE,. that the migration scripts will run during up/down migrations.

### Open Questions
  - Do all of the migrations run automatically when upgrading and downgrading ACM/MCE?
  - Do all of the migrations run automatically when upgrading and downgrading SAAS?

### Answered questions
  - How many active clusters do we have in assisted?
    approximately 3800 clusters
  - How much file data do we have in S3, how many files on average per customer?
    unsure but going with a fair assumption of 10 files per cluster.

### UI Impact

This is a backend change that should have no UI impact, there is no need to change API return values. From an "end user" perspective, nothing has changed.

### Test Plan

The following cases will need to be tested:
	Unit-tests:
		1: Add a new manifest (as an API user), the manifest should be marked as `user-supplied`
		2: Add a new manifest (as an internal user), the manifest should be marked as `system-generated`
		3: Add a mixed collection of system-generated and user-supplied manifests, list them and make sure they are correctly listed
			- Test the default "list only user-supplied" behavior
			- Test the "include system-generated" behavior
		
	Openshift release (end to end) test:
		1: Add a new manifest (as an API user), the manifest should be marked as `user-supplied`
		2: Add a new manifest (as an internal user), the manifest should be marked as `system-generated`
		3: Add a mixed collection of system-generated and user-supplied manifests, list them and make sure they are correctly listed
			- Test the default "list only user-supplied" behavior
			- Test the "include system-generated" behavior
		4: Add a mixed collection of system-generated and user-supplied manifests, some patch manifests also, ensure that the resulting cluster has consumed the manifests correctly.

#### Manual tests - scenarios

* A manual test of migrating from a previous version of assisted should be performed on an assisted-test-infra cluster running minio
* A manual test of migrating to a previous version from the latest version of assisted should be performed on an assisted-test-infra cluster running minio

The following manual test cases should be performed for each scenario

* Add a mixed collection of system-generated and user-supplied manifests, list them and make sure they are correctly listed
			- Test the default "list only user-supplied" behavior
			- Test the "include system-generated" behavior

#### Migration test
- A migration test should be performed in SAAS staging to ensure that every cluster has manifests modified correctly when migrating up/down
- A migration test should be performed by QA in the ACM/MCE environment to ensure that custom manifests continue to work correctly after migration up/down

## Drawbacks

A data migration will need to be performed. A migration script will need to be written. Though I think this disadvantage is outweighed by the reliability benefits this will bring.

## Alternatives

- A transactional database could be used to track manifest metadata but this would be more complicated than the current scenario and with fewer benefits.
- User manifests could be stored in an independent directory such as `openshift.userprovided` and `manifests.userprovided` but this is not very flexible and is hard to maintain.
