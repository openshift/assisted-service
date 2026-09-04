---
title: handling-multiple-huge-images
authors:
  - "@aantal"
creation-date: 2026-09-05
last-updated: 2026-09-05
---

# Image service handling huge OVE images

## Motivation
Right now the assisted-image-service stores every image on it's backend storage.
A new requirement is to serve multiple (2 Z stream release for every supported Y stream starting from 4.22) OVE images. An OVE image is 50-60 Gi.
This puts an enermous load on the backend storage. It halso has a negative effect on booting the pod. Each image which is available in OS_IMAGES environemt variable,
but missing from the storage needs to be downloaded. The plan is to release a new Z release on a weekly basis. This means that multiple images are going to be downloaded, stored and served.
Currently assisted-image-service does not have the capability to store the images in a shared storage, meaning every pod in a deployment (defaulting to 3) has it's own copy of the same immutable image, which is a vaste of storage.

### Goals
* Storing images remotly in an http(s) server or in some object storage
* Avoid storing the same immutable image multiple times
* Keeping the boot time as minimal as possible
* Having reasonable download times

## Proposals
The idea is to store every but the OVE images locally. OVE images could be streamed from a remote http(s) server.
#### Pros
* The big images aren't needed to be downloaded.
* The other images are served in the already proven way
* No additional storage costs
* Relatively quick boot time
* No race condition downloading new images and deleting the old ones at boot time
### Cons
* Medium, high implementation cost. Implementing this solution would need to implement the io.ReadSeeker Go interface.
* The http server can be a bottle neck
* Slower response time

Because the big OVE image does not need to be downloaded at boot the boot time becomes significantly faster. Since the ISO can be streamed from the server where it gets released there is no additional storage cost.
The serving http server can be a bottle neck. If it cannot keep up with the demand of the deployed assisted-image-service pods then a new server needs to be deployed, which will add the existing storage cost. This would bring a dependency on that additional server.
Since the image-service does not maintain the http server hosting the OVE images there are no race conditions at boot time, like more pods are writing the same image, or an image used by another pod is being deleted. Thus there is no need creating a leader election to synchronize such events. If the shared storage is maintained by the assisted-image-service then such issues needs to be addressed.

When an HTTP server supports the Accept-Ranges: bytes header (which almost all static file servers and CDNs do), a client can request a specific byte chunk by sending a header like Range: bytes=1000-2000.
To avoid downloading the ISO, a custom Go struct has to be implemented that satisfies the io.ReadSeeker interface but translates those calls into HTTP requests:
* Seek(offset, whence): Updates an internal integer tracking the "current position." No network call is made yet.
* Read(p []byte): Translates the internal position and the size of p into an HTTP Range request, executes the GET, and writes the response body into p.
Assisted-image-service uses diskfs for costumizing the ISO9660 filesystem. To find the offset of /images/ignition.img or other files diskfs must read the ISO header, jump over volume descriptor, jump to the root directory and traverse the filesystem tree. This requires multiple read and seek operations.
Once the boundaries are found the image can be seemlesly streamed, with the overlay injecting the custom data as the stream passes those byte offsets.
To overcome the constant exploration of the ISO filesystem (discovering the boundaries), the offsets of the required files can be cashed (the ISO is immutable, meaning that the data and it's location does not change).
When an offline image is requested the flow looks like:
* one http stream is opened
* in a loop
** reads the data from the http server into a small buffer
** the content of that buffer is sent to the reciever
* the loop ends when the first boundary is reached (this is tipically the ignition config, and usually this is the only custom chunk)
* stream closes
* the ignition config is served from the memory, if the custom iginition is shorter then the maximum length it is padded, so the structure of the ISO would be consistent
* new stream is opened
* in a loop
** data is read from the http server to a small buffer
** the content of that buffer is sent to that reciever
* the loop ends when either the next boundary is reached or the end of the ISO
* stream closes
* the process is repeated until the whoe ISO is served


### User Stories
Deploying assisted-image-service with reasonable boot time and reasonable amount of resources. User should be given the choice if they want to store the OVE images locally or in some remote place like http(s) server or AWS S3.

### Risks and Mitigations
* the http server cannot handle the load. In fact we should mirror the original http server so the testing would not put any additional load on the http server serving the production
* if the http server is backed by an AWS (or other cloud service) the costs can significantly increase
* the resources of the entity hosting assisted-image-service can be streched, mainly network capacity and CPU. When the image is served from a local storage the it has network connection only to one direction. But when it is streamed it has to be downloaded too, doubling the network trafic. It puts additional load to the CPU too.

## Design Details

### UI Impact
UI needs to properly serve the link where the image is streamed from.

### Test Plan
Since this is a major change in the work flow an extensive testing is needed.
* test if a disconnected and a non-disconnected image exists with the same openshift version and cpu architecture (disconnected image should not be streamed from the backend storage, non-disconnected image shold not be tried to be streamed from a remote http server)
* stress test would be needed (at least 30 parallel download of the OVE image, while serving the other images too)
* e2e disconnected deployment with the downloaded OVE image

## Drawbacks

## Alternatives
AWS S3 native approach
This approach is similar to http streaming, because the data lives in AWS cloud and not on local storage. If it's fetched by Go SDK the data is streamed directly to the application's RAM. It would also need to implement the io.ReadSeeker Go interface.
There are two main things to consider here:
* If S3 is going to be a shared storage or a separate bucket is dedicated for each pod
* If the content of S3 is managed by assisted-image-service or a separate process

This brings us with the following cases:
* S3 is a shared storage and the OVE imaged are managed by assisted service:
** Boot time is slow
** Syncronizing the image download and deletion is a must. At first boot we need only one pod to download one image. The list of OVE images can be split accross the pods to ease the CPU/RAM utilization of one pod. At first boot deleting images is not an issue, since there are no image, or no pod is using those images. If an existing deployment is being updated then OpenShift updates pods one-by-one, meaning that the first pod downloads the new images (no other pod is downloading images at that time since the function Populate() is a blocking operation, pod does not get ready until it's done) and the second, third pod in the row sees that image and skips the download. But the problem arrises when the first pod wants to delete the images which are missing from the OS_IMAGES list. The remaining pod might want to serve those images still. To overcome this issue the leader election offered by OpenShift can be a handy solution. The leader would need to know when all the pods are ready, then the unused images can be deleted.
* S3 is a shared storage and the OVE images are managed by another service:
** Boot time is reasonable (10-20 minutes, if only the OVE images are stored in S3, if all then a couple seconds)
** The assisted-image-service pods can only be started after the images in S3 are up to date.
** Requires synchronization between the service and the deployment of assisted-image-service
** The service needs to know when all the assisted-image-service pods become ready so it could start deleting the images which are not present in OS_IMAGES.
* S3 is dedicated for each pod and the images are managed by assisted-image-service
** The slowest boot time. All the pods needs to download the huge images. If an existing deployment is being updated then each new pod starts sequentially after the previous one becomes ready. Thus it tripples the boot time.
** High storage costs
** No need for synchronization
* S3 is dedicated for each pod and the images are managed by another service:
** Boot time can be fast
** High storage cost
** The image service needs to know when the new images are available
** The service needs to know when the assisted-image-service pod is ready so it could start deleting the images which are not present in the OS_IMAGES.