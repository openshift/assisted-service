---
title: image-handling-service
authors:
  - "@carbonin"
creation-date: 2021-07-06
last-updated: 2021-07-19
---

# Image Handling Service

## Summary

Creating and serving the discovery image is a resource intensive operation which is
significantly different from the other responsibilities of the assisted service.
To remove the performance hit on the rest of the application and to
independently scale these two operations the image handling functionality should
be split into a separate service. This service will be responsible for creating
and serving the discovery image.

## Motivation

Most the assisted service functionality is based on many quick API calls with
relatively small responses. Handling images is all about larger chunks of data and
potentially longer operations which impacts the performance of the rest of the
application. Splitting these responsibilities would allow us to manage and scale
these deployments separately.

### Goals

- Allow image serving to be scaled independently of the rest of the application.
- Remove performance impact due to generating and serving images from the assisted service.
- Simplify assisted service codebase
- Make minimal changes to existing (or proposed) APIs.

### Non-Goals

- Serve a discovery image directly to a BMC

## Proposal

The new service (image service) will handle customizing and serving the customized
image to users. It will query assisted service to retrieve the information that needs
to be added to the image based on the information passed in the request by the user.

The assisted service will retain the `POST /clusters/{cluster_id}/downloads/image`
endpoint which will save the image information to its database. In the case
of the kube API, the controllers will continue to save image information from the
various CRs as they do currently.

The image service will expose a single API endpoint to download an image. Initially
this should be compatible with the existing `GET /clusters/{cluster_id}/downloads/image`
assisted service endpoint. When a user makes a request to this endpoint the
image service will fetch the ignition and ramdisk (if necessary) from the
assisted service and stream the customized iso to the client using the
appropriate template image as a base. This will involve adding an endpoint to
assisted service to expose the ramdisk data.

The image service will be built as a standalone application with its own
repository and tests. This will allow for the cleanest separation of
responsibilities between the two services as well as make the distinction more
clear to developers and reviewers. This should also make tests faster for
assisted service as it won't have to handle iso download/upload on service
startup or image generation tests.

### User Stories

#### Story 1

As a user deploying and running assisted service, I need image serving to be
horizontally scalable and to not impact performance of the assisted service API.

#### Story 2

An additional deployment and service will be managed for all deployment methods.
This includes SaaS, operator, and CI (minikube). Whether an additional image is
created is still an open question.

### Implementation Details/Notes/Constraints

#### Request Flow

1. Image service receives a request to download an image

- This request contains as query parameters, the assisted service api key,
  the image type (minimal/full), and image version

2. Image service queries assisted service for the image ignition
3. Assisted service generates the ignition and serves it
4. If the iso is minimal, the image service queries the assisted service for the initrd
5. Assisted service generates the initrd and serves it
6. Image service streams base iso with ignition and initrd embedded to the user

The image service will receive requests through the existing assisted service
route, but will add [path based route configuration](https://docs.openshift.com/container-platform/4.7/networking/routes/route-configuration.html#nw-path-based-routes_route-configuration)
to move the traffic to the new service. Communication between the assisted
service and the image service will be encrypted using [service serving
certificates](https://docs.openshift.com/container-platform/4.7/security/certificates/service-serving-certificate.html#add-service-serving)
and will use the cluster local service names (rather than the route).

#### API Endpoint

The image service will expose a single API endpoint:

`GET /images/{cluster_id}?version={base_image_verison}&type={image_type}&api_key={api_key}`

The cluster ID and api key will allow the image service to query the assisted
service for the ignition and ramdisk. The image type and version will dictate
which base image should be used.

This request handler should also read any `Authorization` header and pass that
on to any query to the assisted service to ensure authentication works correctly
for downloads in the cloud. Accordingly this means that `api_key` should be
optional as it will only be used for non-cloud deployments.

#### Authentication and Authorization

The image service will not need to implement authentication or authorization
directly as it doesn't manage any user data. The user will provide a token to
the image service and the image service will directly pass that token to
assisted service on each call. Assisted service will validate the token before
giving the image service any user information.

#### Template Images

Currently, when the assisted service starts, it downloads and caches each RHCOS
image specified in the `OPENSHIFT_VERSIONS` environment variable. It also
creates a minimal iso image for each version by removing the root filesystem.
These "template" images are the base for creating the discovery iso.

The image service will take on the responsibility of managing the RHCOS template
images and creating the minimal ISO template images. This means that the image
service will require persistent storage (when not using S3).

#### Image Streaming

The version of the image service described here would require editing the image
while streaming the download rather than the current behavior which stores the
customized image to be downloaded in a separate call. Allowing the customized
discovery ISO to be stored would complicate the proposal and is discussed in the
"Alternatives" section.

#### Assisted Service API Changes

For the image service to create the iso it needs some information from the
assisted service. In the minimal ISO case, it will need the ignition and, if the
user has configured static networking, an additional ramdisk image. For the full
ISO, only the ignition is needed.

Today the ignition is created and uploaded separately to S3 by the assisted
service when the discovery image is generated. The ISO can then be downloaded
using a presigned S3 URL and the ignition is made available for download
through the `/clusters/{cluster_id}/downloads/files` endpoint.

This proposal involves changing that behavior. As mentioned above, the ISO will
be downloaded through the image service. The ignition will no longer be uploaded
to storage, but will be rendered and served by the assisted service on-demand.
An additional API for the minimal ISO ramdisk will behave in the same way.

#### Upgrade Considerations

For the operator-managed deployment, the new version of the operator will deploy
the image service alongside assisted service and configure both to be properly
aware of the other as well as alter the route to push download traffic to the
image service. On the first InfraEnv controller reconcile, all the iso download
URLs will be updated to reference the image service path, this change will then
propagate to the BMH through the Bare Metal Agent controller. Any active agents
will be restarted and will boot from the new image created by the image service.

For the cloud, the existing images will continue to exist through direct
presigned S3 links, but as this proposal removes the need for a "generate image"
step, we will need to update those saved download URLs using a migration.

### Risks and Mitigations

Relying strictly on streaming will increase the load on the image service in
the SaaS deployment as presigned links directly to S3 would no longer be provided.
Downloading the image through the service is already required when running
on a local cluster so this shouldn't be a significant problem, but should
be load tested. This will be further mitigated by splitting the service as
many downloads won't affect the other operations of assisted service.

## Design Details [optional]

### Open Questions

### UI Impact

Allowing only streaming would mean that the discovery image download modal would
need to change to remove the presigned S3 URL and instead point users to the
service directly.

Any existing workarounds regarding time between generating images can be removed
when images are no longer being generated in the background.

### Test Plan

The image service can, and should, be tested in isolation using a mock assisted
service API which will return some image info.

As mentioned previously, load testing will also be needed on the SaaS
deployment to ensure it can handle whatever peak image download load is
expected.

## Drawbacks

- Maintaining and deploying an entirely separate service is complicated.
- No longer saving the image makes any future requirement to serve directly to a
  BMC much more difficult to implement.
- Adds traffic and load to the SaaS by removing images from S3.

## Alternatives

### Streaming vs Storing

This proposal could be amended to handle either streaming or storing an image,
but it would complicate authentication and authorization.

In the current proposal every call to the image service would require a call
to assisted service to fetch customization information. If the image is
generated and stored by the image service, the download call will not need a
call to assisted service and would require some other means of authentication
and authorization.

### Scaling Assisted Service as-is

Many of the goals of this proposal could be achieved by running multiple
assisted service instances in parallel. One for handling image
generation/download and another for serving all other requests. This behavior
would to be dictated by special routing rules and feature gates.

While possible, this would involve adding more logic and behavior switches to
assisted service rather than simplifying the application. Additionally, assisted
service is already difficult to scale in its current form. Each replica needs
to take part in a leader election process to deal with database migration,
downloading template images, and running controllers.

Additionally, creating customized CoreOS ISOs is a fairly common task and
splitting this into its own service and repository allows the logic to be as
consumable as possible by other projects. Embedding this behavior into
assisted service would limit collaboration and adoption by other teams.
