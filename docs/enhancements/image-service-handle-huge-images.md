---
title: handling-multiple-huge-images
authors:
  - "@aantal"
creation-date: 2026-09-05
last-updated: 2026-09-05
---

# Image service handling huge OVE images

## Motivation
Right now the assisted-image-service stores every image on its backend storage.
A new requirement is to serve multiple (2 Z stream release for every supported Y stream starting from 4.22) OVE images. An OVE image is 50-60 Gi.
This puts an enormous load on the backend storage. It also has a negative effect on booting the pod. Even with fast network it can easily add 20 minutes (by my experinece it's usually 30+ minutes) to the boot time of each replica. With rolling update it's about 1 hour so 2 servicable replicas would be available in the deployment. Each image which is available in OS_IMAGES environment variable, but missing from the storage needs to be downloaded. The plan is to release a new Z release on a weekly basis. This means that multiple images are going to be downloaded, stored and served.
Currently assisted-image-service does not have the capability to store the images in a shared storage, meaning every pod in a deployment (defaulting to 3) has its own copy of the same immutable image, which is a waste of storage.

### Goals
* Storing the images in an efficient way
* Avoid storing the same immutable image multiple times
* Keeping the boot time as minimal as possible
* Having reasonable download times
* The solution will not impact the current download time for non-OVE images

### Non-Goals
To make assisted-image-service to be statefull.

## Proposals
The idea is to give the user a choice store the OVE images locally or in a remote location. If images are stored in a remote location then the boot time can be really fast (from seconds to minutes depending on if all the images are stored in a remote location or just the OVE ones).
The idea behind this proposal is that the remote server is not backed by any expensive storage solution.
If it is backed by an expensive storage solution like S3 we end up in a situation shown in "S3 is a shared storage and the OVE images are managed by another service"
### Pros
* The big images aren't needed to be downloaded.
* The other images are served in the already proven way
* Relatively quick boot time
* No race condition downloading new images and deleting the old ones at boot time
### Cons
* Medium, high implementation cost. Implementing this solution would need to implement the io.ReadSeeker Go interface.
* The http server can be a bottleneck
* Slower response time

Because the big OVE image does not need to be downloaded at boot the boot time becomes significantly faster.
The serving http server can be a bottleneck. If it cannot keep up with the demand of the deployed assisted-image-service pods then a new server needs to be deployed, which will add the existing storage cost. This would bring a dependency on that additional server.
Since the image-service does not maintain the http server hosting the OVE images there are no race conditions at boot time, like more pods are writing the same image, or an image used by another pod is being deleted. Thus there is no need creating a leader election to synchronize such events. If the shared storage is maintained by the assisted-image-service then such issues needs to be addressed.

When an HTTP server supports the Accept-Ranges: bytes header (which almost all static file servers and CDNs do), a client can request a specific byte chunk by sending a header like Range: bytes=1000-2000.
To avoid downloading the ISO, a custom Go struct has to be implemented that satisfies the io.ReadSeeker interface but translates those calls into HTTP requests:
* Seek(offset, whence): Updates an internal integer tracking the "current position." No network call is made yet.
* Read(p []byte): Translates the internal position and the size of p into an HTTP Range request, executes the GET, and writes the response body into p.
** The implementation has to make sure that the http server accepted the range request and it's not serving the whole ISO in one.
Assisted-image-service uses diskfs for customizing the ISO9660 filesystem. To find the offset of /images/ignition.img or other files diskfs must read the ISO header, jump over volume descriptor, jump to the root directory and traverse the filesystem tree. This requires multiple read and seek operations.
Once the boundaries are found the image can be seamlessly streamed, with the overlay injecting the custom data as the stream passes those byte offsets.
To overcome the constant exploration of the ISO filesystem (discovering the boundaries), the offsets of the required files can be cached (the ISO is immutable, meaning that the data and its location does not change).
Cashing would work by taking advantage of the ISO filesystem. The ISO filesystem has volume descriptors, directory trees. A directory tree is a record with the list of it's directory records. A directory record has it's name (file name) it's offset and size. To determine the offset those entries have to be traversed recursively. It requires a dozen http request to get the offset of a file, but the whole image does not need to be downloaded.
When an offline image is requested the flow looks like:
* one http stream is opened
* in a loop
** reads the data from the http server into a small buffer
** the content of that buffer is sent to the receiver
* the loop ends when the first boundary is reached (this is typically the ignition config, and usually this is the only custom chunk)
* stream closes
* the ignition config is served from the memory, if the custom ignition is shorter than the maximum length it is padded, so the structure of the ISO would be consistent
* new stream is opened
* in a loop
** data is read from the http server to a small buffer
** the content of that buffer is sent to that receiver
* the loop ends when either the next boundary is reached or the end of the ISO
* stream closes
* the process is repeated until the whole ISO is served


### User Stories
Deploying assisted-image-service with reasonable boot time and reasonable amount of resources. User should be given the choice if they want to store the OVE images locally or in some remote place like http(s) server or AWS S3.

### Risks and Mitigations
* the http server cannot handle the load. In fact we should mirror the original http server so the testing would not put any additional load on the http server serving the production
* if the http server is backed by an AWS (or other cloud service) the costs can significantly increase
* the resources of the entity hosting assisted-image-service can be stretched, mainly network capacity and CPU. When the image is served from a local storage the it has network connection only to one direction. But when it is streamed it has to be downloaded too, doubling the network traffic. It puts additional load to the CPU too.
* the service will become more exposed to network glitches. If everything is stored locally then the service is only vulnerable during its boot time. But when some files are stored remotely then a glitch can make serving the image become broken any time (assuming that the connection between the service and the client is still working).

## Design Details

### UI Impact
UI needs to properly serve the link where the image is streamed from.

### Test Plan
Since this is a major change in the work flow an extensive testing is needed.
* test if a disconnected and a non-disconnected image exists with the same openshift version and cpu architecture (disconnected image should not be streamed from the backend storage, non-disconnected image should not be tried to be streamed from a remote http server)
* stress test would be needed (at least 30 parallel download of the OVE image, while serving the other images too)
* e2e disconnected deployment with the downloaded OVE image

## Drawbacks

## Alternatives
AWS S3 native approach
This approach is similar to http streaming, because the data lives in AWS cloud and not on local storage. AWS S3 is built natively on top of http/https protocol. It means that the standart http operations are working on S3 objects.
There are two main things to consider here:
* If S3 is going to be a shared storage or a separate bucket is dedicated for each pod
* If the content of S3 is managed by assisted-image-service or a separate process

This brings us with the following cases:
* S3 is a shared storage and the OVE images are managed by assisted service:
** Boot time is slow
** Synchronizing the image download and deletion is a must. At first boot we need only one pod to download one image. The list of OVE images can be split across the pods to ease the CPU/RAM utilization of one pod. At first boot deleting images is not an issue, since there are no image, or no pod is using those images. If an existing deployment is being updated then OpenShift updates pods one-by-one, meaning that the first pod downloads the new images (no other pod is downloading images at that time since the function Populate() is a blocking operation, pod does not get ready until it's done) and the second, third pod in the row sees that image and skips the download. But the problem arises when the first pod wants to delete the images which are missing from the OS_IMAGES list. The remaining pod might want to serve those images still. To overcome this issue the leader election offered by OpenShift can be a handy solution. The leader would need to know when all the pods are ready, then the unused images can be deleted.
* S3 is a shared storage and the OVE images are managed by another service:
** Boot time is reasonable (10-20 minutes, if only the OVE images are stored in S3, if all then a couple seconds)
** The assisted-image-service pods can only be started after the images in S3 are up to date.
** Requires synchronization between the service and the deployment of assisted-image-service
** The service needs to know when all the assisted-image-service pods become ready so it could start deleting the images which are not present in OS_IMAGES.
* S3 is dedicated for each pod and the images are managed by assisted-image-service
** The slowest boot time. All the pods needs to download the huge images. If an existing deployment is being updated then each new pod starts sequentially after the previous one becomes ready. Thus it triples the boot time.
** High storage costs
** No need for synchronization
* S3 is dedicated for each pod and the images are managed by another service:
** Boot time can be fast
** High storage cost
** The image service needs to know when the new images are available
** The service needs to know when the assisted-image-service pod is ready so it could start deleting the images which are not present in the OS_IMAGES.

Because making assisted-image-service stateful is out of scope of this enhancement and we want to keep the costs reasonable we are left with the following option (gathered from the previous list):
* S3 is a shared storage and the OVE images are managed by another service


### Pros
* The same implementation of io.ReadSeeker as described in the proposal works
### Cons
* High storage costs
* The content of the bucket has to be maintainded by the owners of assisted-image-service