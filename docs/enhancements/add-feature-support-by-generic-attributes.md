---
title: Add supported-features endpoint
authors:
- "@eliorerz"
  creation-date: 2023-02-23
  last-updated: 2023-03-05
---

# Add supported-features endpoint

## Summary

In the current implementation of Assisted Installer, there is an API that allows the user to get a list of objects
containing a list of features attached to an OpenShift version and its support-level.
Whenever a user wants to know if a feature is supported in an OpenShift version, the user needs to call
the feature-support-list API, get the entire list and filter the results to find the specific feature for that version.
As part supporting multiple CPU architectures on Assisted Installer, we want the ability to add another dimension to the
returned object - CPU architecture, so we can determine if a feature is supported or not by two parameters instead of one.


## Motivation
Adding another attribute to the feature-support-level API is indeed required but in its current implementation there are
few flaws (from my perspective):
1. The current implementation forces us to duplicate the entire features list for each cpu architecture (API change).\
   For example if for `OpenshiftVersion = 4.12` we currently hold 10 features so adding just ARM64 as a CPU architecture to
   that equation will require us to duplicate the 10 features (for that architecture) and set the support-level for both
   x86 and arm64 (note that we also have `s390x` & `ppc64le` architectures).
2. The list is statically defined in assisted-service code so on new OCP version we will have to look at the current release 
   or duplicate the entire features from the previous version even if it supported or not supported for few past versions.
3. It is hard to expand, beside of adding CPU architecture to that equation if in the future we will want to filter by a
   third attribute we will have to duplicate everything again (API change).
4. The user is have to get the entire feature-support list even if he needs to know just the support of a single feature.

These four reasons led me to the realization that we must create a more flexible and easier-to-maintain API.

### Goals

- Add another dimension (of CPU architecture) to the feature-supported endpoint
- Improve the maintainability of this code and less likely to require updates
- Change the API to allow the user to get a list for a specific openshift_version without retrieving the entire list.
- Design an easier way to add a new dimension to that API.

## Proposal

- Add two new API endpoints that will replace the old one (`/v2/feature-support-levels` - Deprecated). 
  The first one is `GET /v2/support-levels/features`, these endpoints will return objects containing features and their support level. 
  Another one is `GET /v2/support-levels/architectures`, these endpoints will return objects containing CPU architectures and their support level.
  Both of those endpoints can be filtered by `openshift_version` and/or `cpu_architecture`.
- Create a GO interface for feature-support level that will allow us to filter the support level of both features and architectures
  by multiple arguments.
- Create a feature-specific object (per feature ID) that will implement the described interface and will return the support level
  of that specific feature.
- The new API will not return a list of filtered architectures or features. The primary filter will be a given `openshift_version`
  (mandatory) and more optional arguments (`cpu_architecture`) 


Implementation Details

### swagger.yaml
```swagger codegen
  /v2/support-levels/features:
    get:
      tags:
        - installer
      security:
        - userAuth: [admin, read-only-admin, user]
      operationId: GetSupportedFeatures
      description: Retrieves the features support levels for each OpenShift version.
      parameters:
        - in: query
          name: openshift_version
          type: string
          description: Version of the OpenShift cluster.
          required: true
        - in: query
          name: cpu_architecture
          description: The CPU architecture of the image (x86_64/arm64/etc).
          type: string
          # TODO: remove arm64 when AI moves to using aarch64
          enum: [ 'x86_64', 'aarch64', 'arm64','ppc64le','s390x','multi' ]

      responses:
        "200":
          description: Success.
          schema:
            type: object
            properties:
              features:
                $ref: '#/definitions/support-levels'
        "401":
          description: Unauthorized.
          schema:
            $ref: '#/definitions/infra_error'
        "403":
          description: Forbidden.
          schema:
            $ref: '#/definitions/infra_error'
        "503":
          description: Unavailable.
          schema:
            $ref: '#/definitions/error'

  /v2/support-levels/architectures:
    get:
      tags:
        - installer
      security:
        - userAuth: [admin, read-only-admin, user]
      operationId: GetSupportedArchitectures
      description: Retrieves the architecture support-levels for each OpenShift version.
      parameters:
        - in: query
          name: openshift_version
          type: string
          description: Version of the OpenShift cluster.
          required: true
      responses:
        "200":
          description: Success.
          schema:
            type: object
            properties:
              architectures:
                $ref: '#/definitions/support-levels'
        "401":
          description: Unauthorized.
          schema:
            $ref: '#/definitions/infra_error'
        "403":
          description: Forbidden.
          schema:
            $ref: '#/definitions/infra_error'
        "503":
          description: Unavailable.
          schema:
            $ref: '#/definitions/error'

  support-levels:
    type: object
    description: Map of featureID alongside their support level
    additionalProperties:
      $ref : '#/definitions/support-level'

  support-level:
    type: string
    enum: [ supported, unsupported, tech-preview, dev-preview ]

  feature-support-level-id:
    type: string
    enum:
      - 'ADDITIONAL_NTP_SOURCE'
      - 'REQUESTED_HOSTNAME'
      - 'PROXY'
      - 'SNO'
      - 'DAY2_HOSTS'
      - 'VIP_AUTO_ALLOC'
      - 'DISK_SELECTION'
      - 'OVN_NETWORK_TYPE'
      - 'SDN_NETWORK_TYPE'
      - 'PLATFORM_SELECTION'
      - 'SCHEDULABLE_MASTERS'
      - 'AUTO_ASSIGN_ROLE'
      - 'CUSTOM_MANIFEST'
      - 'DISK_ENCRYPTION'
      - 'CLUSTER_MANAGED_NETWORKING_WITH_VMS'
      - 'X86_64_ARCHITECTURE'                                 # New
      - 'ARM64_ARCHITECTURE'
      - 'PPC64LE_ARCHITECTURE'
      - 'S390X_ARCHITECTURE'
      - 'SINGLE_NODE_EXPANSION'
      - 'LVM'
      - 'DUAL_STACK_NETWORKING'
      - 'MULTIARCH_RELEASE_IMAGE'
      - 'NUTANIX_INTEGRATION'
      - 'DUAL_STACK_VIPS'
      - 'USER_MANAGED_NETWORKING_WITH_MULTI_NODE'
      - 'CLUSTER_MANAGED_NETWORKING'                          # New
      - 'ARM64_ARCHITECTURE_WITH_CLUSTER_MANAGED_NETWORKING'  # Deprecated

```

### features.go
```go
func GetFeatureSupportLevel(openshiftVersion string, featureId models.FeatureSupportLevel, architecture string) models.SupportLevel {
	return FeaturesList[featureId].GetSupportLevel(openshiftVersion, architecture)
}
```


### features.go
```go
type SupportLevelFeature interface {
  GetId() models.FeatureSupportLevel
  GetSupportLevel(openshiftVersion, architecture string) models.SupportLevel
}

type SupportLevelFilters struct {
  OpenshiftVersion string
  CPUArchitecture  *string
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

func (feature *NutanixIntegrationFeature) GetId() models.FeatureSupportLevelID {
  return models.FeatureSupportLevelIDNUTANIXINTEGRATION
}

func (feature *NutanixIntegrationFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
  if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
    return models.SupportLevelUnsupported
  }

  if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
    return models.SupportLevelDevPreview
  }
  return models.SupportLevelSupported
}
```


### User Stories

#### Story 1

As a developer I want to be able to easily filter the feature-support level by openshift_version and multiple attributes.

### Risks and Mitigations

This is a new API so this doesn't contain any risks to the currrent flow.

### UI Impact

UI will not direct take impact from this change.\
In order to support the new feature, UI will need to use the new API and pass the appropriate parameters.

### Test Plan

- Create unittests and subsystem-tests for all existing feature support combinations.
