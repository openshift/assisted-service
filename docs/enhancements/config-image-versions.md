---
title: config-image-versions
authors:
  - "@lranjbar"
creation-date: 2021-10-27
last-updated: 2021-10-29
---

# Configurable Image Versions

## Summary

When deploying Assisted Installer a ConfigMap is required to do so. In this
ConfigMap we have the values of various components of Assisted Installer including
the images for the application. In the code for assisted-service, we define
several environment variables for these images. We also provide default 
values for these images which can differ from the desired value at runtime.

For many container based software applications the version of the container ran 
is determined at runtime. Consider the simple example of `docker run myrepo/myimage` 
the docker client will resolve this to `hub.docker.com/myrepo/myimage:latest` and 
then run the image on your system. The expectation is that image ran would be the 
version you supplied using latest as the default tag.

If we expand this example out to container orchestration technologies, like Openshift,
the expectation is that the version supplied to Openshift would be the version 
that is ran for all images and containers. The expectation is that if the version
you supplied to Openshift doesn't exist then it would fail to pull the image and 
throw the `ImagePullBackOff` error.

## Motivation

### Goals

The main goal of this proposal is to have the ability to control the images used in 
an Assisted Installer deployment using standard container deployment methods.

### Non-Goals

There are default versions of Openshift Container Platform and RHCOS images
supplied to Assisted Installer. These are not the focus of this discussion only
how we decide which images to deploy and interact with.

## Proposal

First, if we need to populate these environment variables in the code then 
Assisted Installer should populate them with the environment variables given 
in the ConfigMap at runtime.

Second, have errors thrown when the environment variables for the images are 
blank and not provided in the ConfigMap. If a user doesn't supply a value for a 
required image we should give a runtime error for the user.

Third, throw warnings (or errors) when the version in the environment variable 
differs from what is supplied at runtime for Assisted Installer.

Finally, it might be good to validate that all the version tags of Assisted 
Installer match in a deployment. The versions of Assisted Installer are not 
backwards compatible with each other.

### User Stories

#### Story 1

As a user of Assisted Installer I expect the images provided in the ConfigMap
to be the ones that are used by Openshift in the deployment.

#### Story 2

As a user of Assisted Installer I expect to have basic validations on missing
values in the ConfigMap.

#### Story 3

As a user of Assisted Installer I expect to be warned if my image configuration
drifts during runtime. (This could be a sign of instability of my installation.)

#### Story 4

As a user of Assisted Installer I expect to be warned of running incompatible 
versions of components with each other.

### Risks and Mitigations

It seems that these default values for the Assisted Installer images, are probably
viewed as a mitigation to a user not providing a value for the image to run.

Now that there are now several versions of Assisted Installer that are not compatible 
with each other, I ask the team to rethink this. The version of these images to run 
is determined at runtime by the user. When writing the code we do not know this
value.

By supplying default values we run the risk of trying to run components that are 
incompatible with each other.

### Open Questions

- Is the reason these default image values are supplied because it is used in code
logic? Are we looking at needing something like an API endpoint for our components 
that would omit the version running?

### UI Impact

For most microservice architectures this would not have a UI impact. Though putting
a warning in the UI somewhere might make sense for story #4.

### Test Plan

- To test this change we would need to make e2e tests with different deployment 
configurations.

- Unit tests that currently assume these image versions would have to be reworked.

## Drawbacks

Hinted at in the open questions, it isn't clear why these values need to be supplied.
If we have code logic using image values for something, then that needs to be
addressed in this change.

## Alternatives

- Update the image tags in the code every release

We could do the route of leaving most of it as is and updating these values every
release. Big drawback is around how the Operator and Cloud have different versions
and the Cloud releases often enough I would advise against trying this route.

## Notes from Architecture Meeting on 28-10-2021

When discussed in the architecture meeting an implementation detail that was brought
up around the Podman deployment. Currently the podman deployment doesn't have the same
level of configuration files as the Cloud or Operator deployments. Current the Podman 
deployment is the only method that uses these defaults without providing values in
the environment variables. We will need to address this and add these configuration 
options to the Podman deployment option as well.

### Actions Steps

1) Update Podman deployment to have configurable environment variables for these images
2) Update the environment variables to pull from configuration and not have defaults
3) Add in error handling for when these environment variables are empty
4) Update the unit tests to take in a value for these settings
5) Update the generate-all scripts as needed for the deployment scripts